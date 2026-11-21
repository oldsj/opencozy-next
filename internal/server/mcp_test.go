package server

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/authz"
)

// TestMCPToolsMatchTheirOperations: every operation an assistant may call has exactly one tool,
// and every tool's operation allows assistants. A tool that changes something requires a
// requestId; a read has none.
func TestMCPToolsMatchTheirOperations(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	table, err := authz.New(spec)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newMCPCatalog(spec, table)
	if err != nil {
		t.Fatal(err)
	}
	tooled := map[string]bool{}
	for _, b := range catalog.tools {
		tooled[strings.ToLower(b.tool.Op)] = true
		schema := b.def.InputSchema.(map[string]any)
		required, _ := schema["required"].([]string)
		if b.command != slices.Contains(required, "requestId") {
			t.Errorf("%s: command %v, requestId required %v", b.tool.Name, b.command, slices.Contains(required, "requestId"))
		}
		if _, err := json.Marshal(schema); err != nil {
			t.Errorf("%s: schema: %v", b.tool.Name, err)
		}
		if strings.Contains(mustJSON(t, schema), "$ref") {
			t.Errorf("%s: schema keeps a $ref", b.tool.Name)
		}
	}
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			p, _ := table.PolicyOf(op)
			id := strings.ToLower(op.OperationID)
			if slices.Contains(p.Actors, actor.Assistant) && id != strings.ToLower(mcpOp) && !tooled[id] {
				t.Errorf("%s allows assistants but has no MCP tool", op.OperationID)
			}
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// rpc sends one JSON-RPC message to /api/mcp and returns its result (or error) object.
func (e *env) rpc(c cred, method string, params any) (resp, map[string]any) {
	e.t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		msg["params"] = params
	}
	r := e.send(c, "POST", "/api/mcp", msg, "Accept", "application/json, text/event-stream", "Idempotency-Key", "")
	if r.status != 200 {
		return r, nil
	}
	if res, ok := r.body["result"].(map[string]any); ok {
		return r, res
	}
	return r, map[string]any{"rpcError": r.body["error"]}
}

// tool calls one tool and returns its structuredContent, and whether it is an error.
func (e *env) tool(c cred, name string, args map[string]any) (map[string]any, bool) {
	e.t.Helper()
	r, res := e.rpc(c, "tools/call", map[string]any{"name": name, "arguments": args})
	if res == nil {
		e.t.Fatalf("%s: %d %s", name, r.status, r.raw)
	}
	if res["rpcError"] != nil {
		e.t.Fatalf("%s: %v", name, res["rpcError"])
	}
	sc, _ := res["structuredContent"].(map[string]any)
	isErr, _ := res["isError"].(bool)
	return sc, isErr
}

func toolNames(res map[string]any) []string {
	var names []string
	list, _ := res["tools"].([]any)
	for _, t := range list {
		names = append(names, t.(map[string]any)["name"].(string))
	}
	slices.Sort(names)
	return names
}

func toolError(t *testing.T, sc map[string]any) (string, float64) {
	t.Helper()
	e, _ := sc["error"].(map[string]any)
	code, _ := e["code"].(string)
	status, _ := e["status"].(float64)
	return code, status
}

func TestMCP(t *testing.T) {
	e := newEnv(t, nil)
	scopes := e.srv.table.Scopes()
	h := e.house("a", scopes)
	other := e.house("b", scopes)
	assistant := h.actors["assistant"]
	var via uuid.UUID
	if err := e.owner.QueryRow(t.Context(), `SELECT id FROM credential WHERE household_id = $1 AND kind = 'assistant'`, h.id).Scan(&via); err != nil {
		t.Fatal(err)
	}

	// Only an assistant: no credential is the one 401, an adult's cookie or a service is 403.
	if r, _ := e.rpc(cred{}, "tools/list", nil); r.status != 401 {
		t.Errorf("no credential: %d", r.status)
	}
	for _, name := range []string{"adult", "phone", "service-all"} {
		if r, _ := e.rpc(h.actors[name], "tools/list", nil); r.status != 403 {
			t.Errorf("%s: %d", name, r.status)
		}
	}
	if r := e.send(assistant, "GET", "/api/mcp", nil); r.status != 405 {
		t.Errorf("GET: %d", r.status)
	}

	r, init := e.rpc(assistant, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "fixture-client", "version": "1"}})
	if info, _ := init["serverInfo"].(map[string]any); info["name"] != "opencozy" || init["instructions"] == nil {
		t.Fatalf("initialize: %d %s", r.status, r.raw)
	}

	// tools/list shows what the scopes allow: everything for this one, two tools for a reader.
	_, list := e.rpc(assistant, "tools/list", nil)
	if got := toolNames(list); len(got) != len(mcpTools) {
		t.Errorf("all scopes: %v", got)
	}
	tok, hash, _ := auth.NewToken(auth.PrefixAssistant)
	e.exec(`INSERT INTO credential (household_id, kind, name, token_hash, scopes, all_children, adult_id)
		VALUES ($1, 'assistant', 'Reader', $2, '{today:read}', true, $3)`, h.id, hash, h.adult)
	reader := cred{bearer: tok}
	_, list = e.rpc(reader, "tools/list", nil)
	if got := toolNames(list); !slices.Equal(got, []string{"describe_access", "get_today"}) {
		t.Errorf("today:read: %v", got)
	}
	if _, res := e.rpc(reader, "tools/call", map[string]any{"name": "complete_chore", "arguments": map[string]any{"choreId": h.chore, "requestId": uuid.NewString()}}); res["rpcError"] == nil && res["isError"] != true {
		t.Errorf("a tool outside the scopes ran: %v", res)
	}
	if sc, isErr := e.tool(reader, "get_today", nil); isErr || sc["children"] == nil {
		t.Errorf("get_today: %v", sc)
	}

	// A command needs a requestId, and unknown arguments are refused.
	if sc, isErr := e.tool(assistant, "complete_chore", map[string]any{"choreId": h.chore}); !isErr {
		t.Errorf("no requestId: %v", sc)
	} else if code, _ := toolError(t, sc); code != "invalid_arguments" {
		t.Errorf("no requestId: %v", sc)
	}
	if sc, isErr := e.tool(assistant, "get_today", map[string]any{"householdId": other.id}); !isErr {
		t.Errorf("a household argument was accepted: %v", sc)
	}

	// The assistant completes a chore as its adult: done at once, the points land once, recorded
	// as the adult via the assistant. The same requestId replays.
	key := uuid.NewString()
	first, isErr := e.tool(assistant, "complete_chore", map[string]any{"choreId": h.chore, "requestId": key})
	if isErr {
		t.Fatalf("complete_chore: %v", first)
	}
	again, isErr := e.tool(assistant, "complete_chore", map[string]any{"choreId": h.chore, "requestId": key})
	if isErr || mustJSON(t, again) != mustJSON(t, first) {
		t.Errorf("replay: %v %v", again, first)
	}
	var awards int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM point_ledger l JOIN occurrence o ON o.id = l.source_id
		WHERE o.definition_id = $1 AND l.kind = 'award' AND l.adult_id = $2 AND l.credential_id = $3`, h.chore, h.adult, via).Scan(&awards); err != nil || awards != 1 {
		t.Errorf("awards via the assistant: %d %v", awards, err)
	}
	var pending int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM approval a JOIN occurrence o ON o.id = a.occurrence_id
		WHERE o.definition_id = $1 AND a.state = 'pending'`, h.chore).Scan(&pending); err != nil || pending != 1 {
		t.Errorf("only the fixture's earlier approval waits: %d %v", pending, err)
	}

	// Approving records the adult, via the assistant.
	if sc, isErr := e.tool(assistant, "approve_inbox_item", map[string]any{"itemId": h.approval, "requestId": uuid.NewString()}); isErr {
		t.Fatalf("approve: %v", sc)
	}
	var decided string
	if err := e.owner.QueryRow(t.Context(), `SELECT state || ' ' || (decided_by = $2)::text || ' ' || (decided_via = $3)::text FROM approval WHERE id = $1`,
		h.approval, h.adult, via).Scan(&decided); err != nil || decided != "approved true true" {
		t.Errorf("decision: %q %v", decided, err)
	}

	// A grant of iPad time records the adult via the assistant, and is not limited by allowed hours.
	if sc, isErr := e.tool(assistant, "grant_screen_time", map[string]any{"childId": h.child1, "minutes": 15, "requestId": uuid.NewString()}); isErr {
		t.Fatalf("grant: %v", sc)
	}
	var grants int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM screen_time_credit WHERE child_id = $1 AND granted_by = $2 AND granted_via = $3 AND minutes = 15`,
		h.child1, h.adult, via).Scan(&grants); err != nil || grants != 1 {
		t.Errorf("grants via the assistant: %d %v", grants, err)
	}

	// Another household's child is not found, and nothing changes there.
	before := e.fingerprint(other.id)
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"get_screen_time", map[string]any{"childId": other.child1}},
		{"grant_screen_time", map[string]any{"childId": other.child1, "minutes": 15, "requestId": uuid.NewString()}},
		{"complete_chore", map[string]any{"choreId": other.chore, "requestId": uuid.NewString()}},
		{"approve_inbox_item", map[string]any{"itemId": other.approval, "requestId": uuid.NewString()}},
	} {
		sc, isErr := e.tool(assistant, call.name, call.args)
		if _, status := toolError(t, sc); !isErr || status != 404 {
			t.Errorf("%s on another household: %v", call.name, sc)
		}
	}
	if e.fingerprint(other.id) != before {
		t.Error("another household changed")
	}

	// Revoking the credential stops it on the next call.
	e.exec(`UPDATE credential SET revoked_at = now() WHERE id = $1`, via)
	if r, _ := e.rpc(assistant, "tools/list", nil); r.status != 401 {
		t.Errorf("revoked: %d", r.status)
	}
}

// TestAssistantCredentialsActForEveryChild: an assistant can't be limited to some children.
func TestAssistantCredentialsActForEveryChild(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", e.srv.table.Scopes())
	adult := h.actors["adult"]
	if r := e.send(adult, "POST", "/api/credentials", map[string]any{"kind": "assistant", "name": "Helper", "scopes": []string{"today:read"}, "childIds": []uuid.UUID{h.child1}}); r.status != 400 {
		t.Errorf("an assistant for one child: %d %s", r.status, r.raw)
	}
	r := e.send(adult, "POST", "/api/credentials", map[string]any{"kind": "assistant", "name": "Helper", "scopes": []string{"today:read", "inbox:decide"}})
	if r.status != 201 {
		t.Fatalf("create: %d %s", r.status, r.raw)
	}
	if c, _ := r.body["credential"].(map[string]any); c["allChildren"] != true {
		t.Errorf("allChildren: %v", c)
	}
}

// TestMCPStdio: `app mcp` serves the same tools as the credential in OPENCOZY_MCP_TOKEN, and
// refuses to start with anything but a live assistant credential.
func TestMCPStdio(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", e.srv.table.Scopes())
	ctx := t.Context()
	if err := e.srv.ServeMCP(ctx, h.actors["service-all"].bearer, &mcp.InMemoryTransport{}); err == nil {
		t.Error("a service credential started stdio")
	}
	if err := e.srv.ServeMCP(ctx, "oca_not-a-real-secret", &mcp.InMemoryTransport{}); err == nil {
		t.Error("an unknown secret started stdio")
	}

	serverSide, clientSide := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- e.srv.ServeMCP(ctx, h.actors["assistant"].bearer, serverSide) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != len(mcpTools) {
		t.Fatalf("tools: %v %v", tools, err)
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "complete_chore", Arguments: map[string]any{"choreId": h.chore, "requestId": uuid.NewString()}})
	if err != nil || res.IsError {
		t.Fatalf("complete_chore: %v %v", res, err)
	}
	var state string
	if err := e.owner.QueryRow(ctx, `SELECT state FROM occurrence WHERE definition_id = $1 ORDER BY day DESC LIMIT 1`, h.chore).Scan(&state); err != nil || state != "complete" {
		t.Errorf("chore: %q %v", state, err)
	}
	_ = session.Close()
	if err := <-done; err != nil {
		t.Logf("stdio ended: %v", err)
	}
}
