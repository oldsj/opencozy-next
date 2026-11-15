package authz

import (
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
		"no x-actors":           "    get: {operationId: a, responses: {'200': {description: ok}}}\n",
		"empty x-actors":        "    get: {operationId: a, x-actors: [], responses: {'200': {description: ok}}}\n",
		"unknown actor":         "    get: {operationId: a, x-actors: [robot], responses: {'200': {description: ok}}}\n",
		"public and actor":      "    get: {operationId: a, x-actors: [public, adult], responses: {'200': {description: ok}}}\n",
		"service without scope": "    get: {operationId: a, x-actors: [service], responses: {'200': {description: ok}}}\n",
		"display without scope": "    get: {operationId: a, x-actors: [display], responses: {'200': {description: ok}}}\n",
		"bad child param":       "    get: {operationId: a, x-actors: [adult], x-child-param: nope, responses: {'200': {description: ok}}}\n",
		"body limit too big":    "    get: {operationId: a, x-actors: [adult], x-body-limit: 99999999, responses: {'200': {description: ok}}}\n",
		"public command":        "    post: {operationId: a, x-actors: [public], x-command: true, responses: {'200': {description: ok}}}\n",
	}
	for name, ops := range cases {
		if _, err := New(load(t, ops)); err == nil {
			t.Errorf("%s: accepted", name)
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
