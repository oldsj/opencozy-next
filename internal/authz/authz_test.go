package authz

import (
	"net/http"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func load(t *testing.T, ops string) *openapi3.T {
	t.Helper()
	doc := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /things/{thingId}:\n    parameters:\n      - {name: thingId, in: path, required: true, schema: {type: string}}\n" + ops
	spec, err := openapi3.NewLoader().LoadFromData([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestNewRefusesUndeclaredOrInconsistent(t *testing.T) {
	cases := map[string]string{
		"no x-actors":            "    get: {operationId: a, responses: {'200': {description: ok}}}\n",
		"empty x-actors":         "    get: {operationId: a, x-actors: [], responses: {'200': {description: ok}}}\n",
		"unknown actor":          "    get: {operationId: a, x-actors: [robot], responses: {'200': {description: ok}}}\n",
		"public and actor":       "    get: {operationId: a, x-actors: [public, adult], responses: {'200': {description: ok}}}\n",
		"service without scope":  "    get: {operationId: a, x-actors: [service], responses: {'200': {description: ok}}}\n",
		"display without scope":  "    get: {operationId: a, x-actors: [display], responses: {'200': {description: ok}}}\n",
		"bad child param":        "    get: {operationId: a, x-actors: [adult], x-child-param: nope, responses: {'200': {description: ok}}}\n",
		"body limit too big":     "    get: {operationId: a, x-actors: [adult], x-body-limit: 99999999, responses: {'200': {description: ok}}}\n",
		"public command":         "    post: {operationId: a, x-actors: [public], x-command: true, responses: {'200': {description: ok}}}\n",
		"command without key":    "    post: {operationId: a, x-actors: [adult], x-command: true, responses: {'200': {description: ok}}}\n",
		"record without command": "    post: {operationId: a, x-actors: [adult], x-record-response: false, responses: {'200': {description: ok}}}\n",
		"recent auth on phone":   "    post: {operationId: a, x-actors: [adult, phone], x-recent-auth: true, responses: {'200': {description: ok}}}\n",
	}
	for name, ops := range cases {
		if _, err := New(load(t, ops)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestNewAcceptsCommandWithKey(t *testing.T) {
	ops := "    post: {operationId: a, x-actors: [adult], x-command: true, x-record-response: false, parameters: [{name: Idempotency-Key, in: header, required: true, schema: {type: string}}], responses: {'200': {description: ok}}}\n"
	tab, err := New(load(t, ops))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range tab.byOp {
		if !p.Command || p.RecordResponse {
			t.Errorf("policy %+v", p)
		}
	}
}

func TestNewAcceptsDeclared(t *testing.T) {
	tab, err := New(load(t, "    get: {operationId: a, x-actors: [adult, service], x-scopes: [things:read], x-child-param: thingId, x-body-limit: 10, responses: {'200': {description: ok}}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tab.Scopes(), ","); got != "things:read" {
		t.Errorf("Scopes() = %q", got)
	}
}

// A command whose answer carries a secret must not be recorded for retries unless it says so.
func TestSecretInCommandResponse(t *testing.T) {
	key := "parameters: [{name: Idempotency-Key, in: header, required: true, schema: {type: string}}]"
	resp := func(schema string) string {
		return "responses: {'201': {description: ok, content: {application/json: {schema: " + schema + "}}}}"
	}
	refused := map[string]string{
		"top level": "{type: object, properties: {secret: {type: string}}}",
		"suffix":    "{type: object, properties: {accessToken: {type: string}}}",
		"nested":    "{type: object, properties: {credential: {type: object, properties: {token: {type: string}}}}}",
		"in a list": "{type: array, items: {type: object, properties: {password: {type: string}}}}",
		"allOf":     "{allOf: [{type: object, properties: {clientSecret: {type: string}}}]}",
	}
	for name, schema := range refused {
		ops := "    post: {operationId: a, x-actors: [adult], x-command: true, " + key + ", " + resp(schema) + "}\n"
		if _, err := New(load(t, ops)); err == nil || !strings.Contains(err.Error(), "x-record-response: false") {
			t.Errorf("%s: %v", name, err)
		}
		ops = "    post: {operationId: a, x-actors: [adult], x-command: true, x-record-response: false, " + key + ", " + resp(schema) + "}\n"
		if _, err := New(load(t, ops)); err != nil {
			t.Errorf("%s with x-record-response: false: %v", name, err)
		}
	}
	// Not a command (nothing is recorded), or no secret: fine.
	for _, ops := range []string{
		"    post: {operationId: a, x-actors: [adult], " + resp("{type: object, properties: {token: {type: string}}}") + "}\n",
		"    post: {operationId: a, x-actors: [adult], x-command: true, " + key + ", " + resp("{type: object, properties: {name: {type: string}, tokenCount: {type: integer}}}") + "}\n",
	} {
		if _, err := New(load(t, ops)); err != nil {
			t.Errorf("refused: %v\n%s", err, ops)
		}
	}
}

func TestFindReportsThePattern(t *testing.T) {
	tab, err := New(load(t, "    get: {operationId: a, x-actors: [adult], responses: {'200': {description: ok}}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	r, _ := http.NewRequest(http.MethodGet, "/things/x", nil)
	p, params, err := tab.Find(r)
	if err != nil || p.Pattern != "GET /things/{thingId}" || params["thingId"] != "x" {
		t.Fatalf("%+v %v %v", p, params, err)
	}
}
