package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/authz"
	"github.com/oldsj/opencozy-next/internal/httpx"
)

// MCP (design §9). POST /api/mcp is an ordinary operation (callMcp, x-actors [assistant]), so the
// request has passed the route, body cap, rate limit, authentication and authorization before
// it reaches the MCP handler. Each tool call then runs its operation through the same API chain
// with the caller's bearer, the requestId as the Idempotency-Key and the caller's address.

const (
	mcpRatePerMinute           = 300 // per client address, like the old app
	mcpCredentialRatePerMinute = 120 // per credential
	mcpMaxResult               = 1 << 20
	mcpOp                      = "callMcp"
)

// mcpCatalog is every tool, ready to list: built once from the spec.
type mcpCatalog struct {
	tools []*mcpBound
}

// mcpBound is a tool bound to its operation.
type mcpBound struct {
	tool        mcpTool
	def         *mcp.Tool
	method      string
	path        string
	scopes      []string
	command     bool
	pathParams  map[string]bool
	queryParams map[string]bool
	bodyProps   map[string]bool
	hasBody     bool
}

// newMCPCatalog binds every tool in mcpTools to its operation and builds its input schema from
// the operation's parameters and body. It fails on a tool whose operation is missing, does not
// allow assistants, or whose argument names collide.
func newMCPCatalog(spec *openapi3.T, table *authz.Table) (*mcpCatalog, error) {
	components := map[string]any{}
	if spec.Components != nil {
		for name, s := range spec.Components.Schemas {
			m, err := schemaMap(s)
			if err != nil {
				return nil, err
			}
			components[name] = m
		}
	}
	type found struct {
		method, path string
		op           *openapi3.Operation
		item         *openapi3.PathItem
	}
	ops := map[string]found{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			ops[strings.ToLower(op.OperationID)] = found{method, path, op, item} // the embedded spec capitalises them
		}
	}
	c := &mcpCatalog{}
	names := map[string]bool{}
	for _, t := range mcpTools {
		f, ok := ops[strings.ToLower(t.Op)]
		if !ok {
			return nil, fmt.Errorf("mcp tool %s: no operation %s", t.Name, t.Op)
		}
		if strings.EqualFold(t.Op, mcpOp) || names[t.Name] {
			return nil, fmt.Errorf("mcp tool %s: invalid or repeated", t.Name)
		}
		names[t.Name] = true
		p, ok := table.PolicyOf(f.op)
		if !ok {
			return nil, fmt.Errorf("mcp tool %s: %s has no policy", t.Name, t.Op)
		}
		if !slices.Contains(p.Actors, actor.Assistant) {
			return nil, fmt.Errorf("mcp tool %s: %s does not allow assistants", t.Name, t.Op)
		}
		b := &mcpBound{tool: t, method: f.method, path: f.path, scopes: p.Scopes, command: p.Command,
			pathParams: map[string]bool{}, queryParams: map[string]bool{}, bodyProps: map[string]bool{}}
		props := map[string]any{}
		var required []string
		add := func(name string, schema any, req bool) error {
			if _, dup := props[name]; dup {
				return fmt.Errorf("mcp tool %s: argument %q appears twice", t.Name, name)
			}
			props[name] = schema
			if req {
				required = append(required, name)
			}
			return nil
		}
		if b.command {
			if err := add("requestId", map[string]any{"type": "string", "format": "uuid",
				"description": "A new UUID for each intended action; repeat it only to retry the same action."}, true); err != nil {
				return nil, err
			}
		}
		params := slices.Concat(f.item.Parameters, f.op.Parameters)
		for _, pr := range params {
			v := pr.Value
			if v == nil || v.In == openapi3.ParameterInHeader || v.In == openapi3.ParameterInCookie {
				continue
			}
			s, err := schemaMap(v.Schema)
			if err != nil {
				return nil, err
			}
			s = inline(s, components, 0)
			if v.Description != "" {
				s["description"] = v.Description
			}
			if err := add(v.Name, s, v.Required || v.In == openapi3.ParameterInPath); err != nil {
				return nil, err
			}
			if v.In == openapi3.ParameterInPath {
				b.pathParams[v.Name] = true
			} else {
				b.queryParams[v.Name] = true
			}
		}
		if rb := f.op.RequestBody; rb != nil && rb.Value != nil {
			if mt := rb.Value.Content.Get("application/json"); mt != nil && mt.Schema != nil {
				s, err := schemaMap(mt.Schema)
				if err != nil {
					return nil, err
				}
				s = inline(s, components, 0)
				bodyProps, _ := s["properties"].(map[string]any)
				bodyReq, _ := s["required"].([]any)
				b.hasBody = true
				for _, name := range slices.Sorted(maps.Keys(bodyProps)) {
					if err := add(name, bodyProps[name], rb.Value.Required && slices.Contains(bodyReq, any(name))); err != nil {
						return nil, err
					}
					b.bodyProps[name] = true
				}
			}
		}
		sort.Strings(required)
		schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if len(required) > 0 {
			schema["required"] = required
		}
		desc := t.Description
		if d := strings.TrimSpace(f.op.Description); d != "" {
			desc += "\n\n" + d
		}
		b.def = &mcp.Tool{Name: t.Name, Description: desc, InputSchema: schema,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: !b.command, IdempotentHint: true, OpenWorldHint: new(false)}}
		c.tools = append(c.tools, b)
	}
	return c, nil
}

// allowed lists the tools a credential with these scopes may see and call.
func (c *mcpCatalog) allowed(a actor.Actor) []*mcpBound {
	var out []*mcpBound
	for _, t := range c.tools {
		if a.Kind == actor.Assistant && a.HasScopes(t.scopes) {
			out = append(out, t)
		}
	}
	return out
}

// schemaMap turns an OpenAPI schema into a JSON value, with references left as $ref.
func schemaMap(s *openapi3.SchemaRef) (map[string]any, error) {
	if s == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// inline replaces #/components/schemas references with their schemas and drops OpenAPI-only
// keywords, so a tool's input schema stands alone.
func inline(v map[string]any, components map[string]any, depth int) map[string]any {
	if depth > 16 {
		return map[string]any{}
	}
	if ref, ok := v["$ref"].(string); ok {
		name, found := strings.CutPrefix(ref, "#/components/schemas/")
		target, _ := components[name].(map[string]any)
		if !found || target == nil {
			return map[string]any{}
		}
		return inline(target, components, depth+1)
	}
	out := make(map[string]any, len(v))
	for k, val := range v {
		if k == "example" || k == "examples" || strings.HasPrefix(k, "x-") {
			continue
		}
		out[k] = inlineValue(val, components, depth)
	}
	return out
}

func inlineValue(val any, components map[string]any, depth int) any {
	switch x := val.(type) {
	case map[string]any:
		return inline(x, components, depth+1)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = inlineValue(e, components, depth)
		}
		return out
	default:
		return val
	}
}

// mcpCaller runs tools for one caller: their bearer, their address, their logger fields.
type mcpCaller struct {
	s      *Server
	actor  actor.Actor
	bearer string
	ip     string
}

// newMCPServer is the MCP server one caller sees: only the tools their scopes allow.
func (s *Server) newMCPServer(c mcpCaller) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "opencozy", Version: "1"}, &mcp.ServerOptions{Instructions: mcpInstructions})
	for _, t := range s.mcp.allowed(c.actor) {
		srv.AddTool(t.def, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return c.call(ctx, t, req.Params.Arguments), nil
		})
	}
	return srv
}

// mcpHandler serves POST /api/mcp after the API chain has authenticated and authorized it.
func (s *Server) mcpHandler() http.Handler {
	perCredential := httpx.NewRateLimiter(mcpCredentialRatePerMinute)
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s.newMCPServer(mcpCaller{s: s, actor: mustActor(r.Context()), bearer: r.Header.Get("Authorization"), ip: httpx.ClientIPFrom(r.Context())})
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := perCredential.Allow(mustActor(r.Context()).CredentialID.String()); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(wait.Seconds()+0.999))))
			httpx.WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again shortly.")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// CallMcp is never reached: apiHandler sends /mcp to mcpHandler before the generated handler.
func (s *Server) CallMcp(context.Context, api.CallMcpRequestObject) (api.CallMcpResponseObject, error) {
	return nil, errors.New("mcp: served by mcpHandler")
}

// call runs one tool: it turns the arguments into its operation's request and runs that through
// the API chain as the caller.
func (c mcpCaller) call(ctx context.Context, t *mcpBound, raw json.RawMessage) *mcp.CallToolResult {
	status, body, err := c.run(ctx, t, raw)
	res := toolResult(status, body, err)
	code := ""
	if res.IsError {
		if m, ok := res.StructuredContent.(map[string]any); ok {
			if e, ok := m["error"].(map[string]any); ok {
				code, _ = e["code"].(string)
			}
		}
	}
	// Who did what, never with the arguments.
	c.s.log.InfoContext(ctx, "mcp.tool_call", "household", c.actor.HouseholdID, "adult", c.actor.AdultID,
		"credential", c.actor.CredentialID, "tool", t.tool.Name, "status", status, "code", code, "client", c.ip)
	return res
}

type argError string

func (e argError) Error() string { return string(e) }

func (c mcpCaller) run(ctx context.Context, t *mcpBound, raw json.RawMessage) (int, []byte, error) {
	args := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return 0, nil, argError("The arguments must be a JSON object.")
		}
	}
	path := t.path
	query := url.Values{}
	body := map[string]json.RawMessage{}
	key := ""
	for _, name := range slices.Sorted(maps.Keys(args)) {
		v := args[name]
		switch {
		case name == "requestId" && t.command:
			var s string
			if json.Unmarshal(v, &s) != nil {
				return 0, nil, argError("requestId must be a UUID.")
			}
			if _, err := uuid.Parse(s); err != nil {
				return 0, nil, argError("requestId must be a UUID.")
			}
			key = s
		case t.pathParams[name]:
			s, ok := scalar(v)
			if !ok {
				return 0, nil, argError(fmt.Sprintf("%s must be a string or number.", name))
			}
			path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(s))
		case t.queryParams[name]:
			var list []json.RawMessage
			if json.Unmarshal(v, &list) == nil {
				for _, e := range list {
					s, ok := scalar(e)
					if !ok {
						return 0, nil, argError(fmt.Sprintf("%s has a value that is not a string or number.", name))
					}
					query.Add(name, s)
				}
				continue
			}
			s, ok := scalar(v)
			if !ok {
				return 0, nil, argError(fmt.Sprintf("%s must be a string, number or boolean.", name))
			}
			query.Set(name, s)
		case t.bodyProps[name]:
			body[name] = v
		default:
			return 0, nil, argError(fmt.Sprintf("Unknown argument %q.", name))
		}
	}
	if t.command && key == "" {
		return 0, nil, argError("requestId is required: a new UUID for this action.")
	}
	if strings.Contains(path, "{") {
		return 0, nil, argError("A required ID is missing.")
	}

	// A body-less request still gets http.NoBody, as a server request would.
	var reader io.Reader = http.NoBody
	if t.hasBody {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(b)
	}
	target := path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	// A fresh context: only the caller's address carries over, never the /mcp request's route,
	// actor or command. It still ends when the tool call does.
	inner, cancel := context.WithCancel(httpx.WithClientIP(context.Background(), c.ip))
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	req, err := http.NewRequestWithContext(inner, t.method, target, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", c.bearer)
	if t.hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := &mcpRecorder{header: http.Header{}}
	c.s.apiChain.ServeHTTP(rec, req)
	return rec.statusOr200(), rec.body.Bytes(), nil
}

// scalar is a JSON string, number or boolean as query or path text.
func scalar(v json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s, true
	}
	var n json.Number
	if json.Unmarshal(v, &n) == nil {
		return n.String(), true
	}
	var b bool
	if json.Unmarshal(v, &b) == nil {
		return strconv.FormatBool(b), true
	}
	return "", false
}

// toolResult turns an API answer into a tool result: the JSON body on success, or
// structuredContent.error {code, status, message} with isError.
func toolResult(status int, body []byte, err error) *mcp.CallToolResult {
	fail := func(status int, code, message string) *mcp.CallToolResult {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}},
			StructuredContent: map[string]any{"error": map[string]any{"code": code, "status": status, "message": message}}}
	}
	var ae argError
	switch {
	case errors.As(err, &ae):
		return fail(http.StatusBadRequest, "invalid_arguments", ae.Error())
	case err != nil:
		return fail(http.StatusInternalServerError, "internal", "Something went wrong.")
	case len(body) > mcpMaxResult:
		return fail(http.StatusInternalServerError, "too_large", "The answer is too large.")
	}
	if status < 200 || status >= 300 {
		var e struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if json.Unmarshal(body, &e) != nil || e.Error.Code == "" {
			e.Error.Code, e.Error.Message = "error", http.StatusText(status)
		}
		return fail(status, e.Error.Code, e.Error.Message)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Done."}}, StructuredContent: map[string]any{"ok": true}}
	}
	var v any
	if json.Unmarshal(body, &v) != nil {
		return fail(http.StatusInternalServerError, "internal", "Something went wrong.")
	}
	if _, ok := v.(map[string]any); !ok {
		v = map[string]any{"items": v}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}, StructuredContent: v}
}

// mcpRecorder keeps an in-process answer, up to one byte past the result cap.
type mcpRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *mcpRecorder) Header() http.Header { return r.header }

func (r *mcpRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *mcpRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if room := mcpMaxResult + 1 - r.body.Len(); room > 0 {
		r.body.Write(b[:min(len(b), room)])
	}
	return len(b), nil
}

func (r *mcpRecorder) statusOr200() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// ServeMCP serves the assistant's tools over t (stdio for `app mcp`) as the assistant credential
// whose secret is token. The tools listed are fixed at start; each call still authenticates, so
// revoking the credential stops the next call.
func (s *Server) ServeMCP(ctx context.Context, token string, t mcp.Transport) error {
	if s.apiChain == nil {
		if _, err := s.Handler(); err != nil {
			return err
		}
	}
	bearer := "Bearer " + token
	probe, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody)
	if err != nil {
		return err
	}
	probe.Header.Set("Authorization", bearer)
	a, ok, err := s.actors.Resolve(ctx, probe)
	if errors.Is(err, actor.ErrUnauthenticated) || (err == nil && (!ok || a.Kind != actor.Assistant)) {
		return errors.New("mcp: OPENCOZY_MCP_TOKEN is not a live assistant credential")
	} else if err != nil {
		return err
	}
	s.log.InfoContext(ctx, "mcp.stdio", "household", a.HouseholdID, "adult", a.AdultID, "credential", a.CredentialID)
	return s.newMCPServer(mcpCaller{s: s, actor: a, bearer: bearer, ip: "stdio"}).Run(ctx, t)
}
