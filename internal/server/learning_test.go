package server

import (
	"fmt"
	"strings"
	"testing"
)

func TestAwardRulesAndLinks(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult := h.actors["adult"]
	r := e.send(adult, "POST", "/api/learning/rules", map[string]any{"activityKey": "read_book", "points": 3, "dailyLimit": 9})
	if r.status != 201 || r.body["version"] != float64(1) || r.body["archived"] != false {
		t.Fatalf("create: %d %s", r.status, r.raw)
	}
	id := r.body["id"].(string)
	if r := e.send(adult, "POST", "/api/learning/rules", map[string]any{"activityKey": "read_book", "points": 1, "dailyLimit": 1}); r.status != 409 || r.code() != "rule_exists" {
		t.Fatalf("duplicate: %d %s", r.status, r.raw)
	}
	edit := map[string]any{"activityKey": "build_house", "points": 4, "dailyLimit": 8, "archived": false, "expectedVersion": 1}
	if r := e.send(adult, "PUT", "/api/learning/rules/"+id, edit); r.status != 409 || r.code() != "rule_exists" {
		t.Fatalf("rename onto another rule: %d %s", r.status, r.raw)
	}
	edit["activityKey"] = "read_book"
	if r := e.send(adult, "PUT", "/api/learning/rules/"+id, edit); r.status != 200 || r.body["version"] != float64(2) || r.body["points"] != float64(4) {
		t.Fatalf("edit: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PUT", "/api/learning/rules/"+id, edit); r.status != 409 || r.code() != "version_conflict" {
		t.Fatalf("stale: %d %s", r.status, r.raw)
	}
	edit["archived"], edit["expectedVersion"] = true, 2
	if r := e.send(adult, "PUT", "/api/learning/rules/"+id, edit); r.status != 200 || r.body["archived"] != true {
		t.Fatalf("archive: %d %s", r.status, r.raw)
	}
	edit["archived"], edit["expectedVersion"] = false, 3
	if r := e.send(adult, "PUT", "/api/learning/rules/"+id, edit); r.status != 409 {
		t.Fatalf("unarchive: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PUT", "/api/learning/rules/"+h.child1.String(), edit); r.status != 404 {
		t.Fatalf("unknown rule: %d %s", r.status, r.raw)
	}
	// The activity is free again; the archived rule shows only when asked for.
	if r := e.send(adult, "POST", "/api/learning/rules", map[string]any{"activityKey": "read_book", "points": 2, "dailyLimit": 2}); r.status != 201 {
		t.Fatalf("recreate: %d %s", r.status, r.raw)
	}
	if n := len(e.send(adult, "GET", "/api/learning/rules", nil).body["rules"].([]any)); n != 2 {
		t.Fatalf("live rules: %d", n)
	}
	if n := len(e.send(adult, "GET", "/api/learning/rules?archived=true", nil).body["rules"].([]any)); n != 3 {
		t.Fatalf("all rules: %d", n)
	}

	link := map[string]any{"childId": h.child2.String(), "provider": "minecraft", "externalId": "Builder_42"}
	if r := e.send(adult, "POST", "/api/learning/links", link); r.status != 409 || r.code() != "link_exists" {
		t.Fatalf("taken account: %d %s", r.status, r.raw)
	}
	link["externalId"] = "Miner_7"
	r = e.send(adult, "POST", "/api/learning/links", link)
	if r.status != 201 || r.body["childId"] != h.child2.String() {
		t.Fatalf("link: %d %s", r.status, r.raw)
	}
	linkID := r.body["id"].(string)
	link["childId"] = h.adult.String()
	link["externalId"] = "Other"
	if r := e.send(adult, "POST", "/api/learning/links", link); r.status != 404 {
		t.Fatalf("unknown child: %d %s", r.status, r.raw)
	}
	if n := len(e.send(adult, "GET", "/api/learning/links", nil).body["links"].([]any)); n != 2 {
		t.Fatalf("links: %d", n)
	}
	if r := e.send(adult, "DELETE", "/api/learning/links/"+linkID, nil); r.status != 204 {
		t.Fatalf("unlink: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "DELETE", "/api/learning/links/"+linkID, nil); r.status != 404 {
		t.Fatalf("unlink again: %d %s", r.status, r.raw)
	}
	// Unlinking only ever sets unlinked_at.
	if _, err := e.owner.Exec(t.Context(), `UPDATE learning_link SET external_id = 'x' WHERE id = $1`, h.link); err == nil {
		t.Fatal("a link's account changed")
	}
}

func TestLearningCredentials(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult := h.actors["adult"]
	body := func(scope string, children, rules []string) map[string]any {
		b := map[string]any{"kind": "service", "name": "Block Buddy", "scopes": []string{scope}, "childIds": children}
		if rules != nil {
			b["awardRuleIds"] = rules
		}
		return b
	}
	kids, rule := []string{h.child1.String()}, []string{h.rule.String()}
	for name, b := range map[string]map[string]any{
		"no rules":         body("learning:award", kids, nil),
		"no children":      body("learning:award", []string{}, rule),
		"rules, no scope":  body("children:read", kids, rule),
		"not a rule":       body("learning:award", kids, []string{h.child2.String()}),
		"assistant awards": {"kind": "assistant", "name": "A", "scopes": []string{"learning:award"}, "childIds": kids, "awardRuleIds": rule},
	} {
		if r := e.send(adult, "POST", "/api/credentials", b); r.status != 400 {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	r := e.send(adult, "POST", "/api/credentials", body("learning:award", kids, rule))
	cred := r.body["credential"].(map[string]any)
	if r.status != 201 || len(cred["awardRuleIds"].([]any)) != 1 {
		t.Fatalf("issue: %d %s", r.status, r.raw)
	}
	list := e.send(adult, "GET", "/api/credentials", nil)
	if !strings.Contains(string(list.raw), `"awardRuleIds":["`+h.rule.String()+`"]`) {
		t.Fatalf("list: %s", list.raw)
	}
}

func TestLearningAwards(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", []string{"learning:award"})
	svc := h.actors["service-all"]
	award := func(c cred, event, account, activity string) resp {
		return e.send(c, "POST", "/api/learning/awards", map[string]any{"sourceEventId": event, "externalId": account, "activityKey": activity}, "Idempotency-Key", event)
	}
	ev := func(n int) string { return fmt.Sprintf("0190a1b2-0000-7000-8000-%012x", n) }

	r := award(svc, ev(0), "Builder_42", "build_house")
	if r.status != 201 || r.body["points"] != float64(5) || r.body["balance"] != float64(10) || r.body["childId"] != h.child1.String() || r.body["ruleVersion"] != float64(1) {
		t.Fatalf("award: %d %s", r.status, r.raw) // balance: the fixture's 5, plus 5
	}
	// Same event again: the same award, even under a new Idempotency-Key.
	again := e.send(svc, "POST", "/api/learning/awards", map[string]any{"sourceEventId": ev(0), "externalId": "Builder_42", "activityKey": "build_house"})
	if again.status != 200 || again.body["id"] != r.body["id"] || again.body["balance"] != float64(10) {
		t.Fatalf("replay: %d %s", again.status, again.raw)
	}
	if r := e.send(svc, "POST", "/api/learning/awards", map[string]any{"sourceEventId": ev(0), "externalId": "Builder_42", "activityKey": "other"}); r.status != 409 || r.code() != "event_reused" {
		t.Fatalf("reused event: %d %s", r.status, r.raw)
	}
	for i, c := range []struct {
		account, activity string
		status            int
		code              string
	}{
		{"Nobody", "build_house", 404, "unknown_learner"},
		{"Builder_42", "fly", 404, "unknown_activity"},
	} {
		if r := award(svc, ev(50+i), c.account, c.activity); r.status != c.status || r.code() != c.code {
			t.Errorf("%s/%s: %d %s", c.account, c.activity, r.status, r.raw)
		}
	}
	// 20 a day at 5 each: three more, then refused.
	for i := 1; i <= 3; i++ {
		if r := award(svc, ev(i), "Builder_42", "build_house"); r.status != 201 {
			t.Fatalf("award %d: %d %s", i, r.status, r.raw)
		}
	}
	if r := award(svc, ev(4), "Builder_42", "build_house"); r.status != 409 || r.code() != "daily_limit_reached" {
		t.Fatalf("over the limit: %d %s", r.status, r.raw)
	}
	// A credential for child1 only can't award child2; one without the rule can't use it.
	e.exec(`INSERT INTO learning_link (household_id, child_id, provider, external_id) VALUES ($1, $2, 'minecraft', 'Miner_7')`, h.id, h.child2)
	if r := award(h.actors["service-child1"], ev(5), "Miner_7", "build_house"); r.status != 403 || r.code() != "child_not_permitted" {
		t.Fatalf("other child: %d %s", r.status, r.raw)
	}
	e.exec(`DELETE FROM credential_award_rule WHERE rule_id = $1`, h.rule)
	if r := award(svc, ev(6), "Miner_7", "build_house"); r.status != 403 || r.code() != "rule_not_permitted" {
		t.Fatalf("no rule: %d %s", r.status, r.raw)
	}
	// (A new Idempotency-Key, so the handler answers, not the recorded response.)
	if r := e.send(svc, "POST", "/api/learning/awards", map[string]any{"sourceEventId": ev(0), "externalId": "Builder_42", "activityKey": "build_house"}); r.status != 403 {
		t.Fatalf("replay after the rule was taken away: %d %s", r.status, r.raw)
	}
	// History names the activity; awards can't be changed.
	hist := e.send(h.actors["adult"], "GET", "/api/children/"+h.child1.String()+"/history", nil)
	if !strings.Contains(string(hist.raw), `"title":"build_house"`) {
		t.Fatalf("history: %s", hist.raw)
	}
	if _, err := e.owner.Exec(t.Context(), `UPDATE learning_award SET points = 1`); err == nil {
		t.Fatal("an award changed")
	}
}
