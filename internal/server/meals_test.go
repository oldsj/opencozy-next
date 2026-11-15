package server

import (
	"strings"
	"testing"

	"github.com/oldsj/opencozy-next/internal/day"
)

func TestRecipesFlow(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", []string{"meals"})
	adult := h.actors["adult"]
	r := e.send(adult, "POST", "/api/recipes", map[string]any{
		"title": "  Lentil soup ", "servings": 4,
		"ingredients": []string{"1 cup red lentils", " 1 onion "}, "steps": []string{"Soften the onion.\nSlowly.", "Simmer."},
	})
	if r.status != 201 || r.body["title"] != "Lentil soup" || r.body["version"] != float64(1) {
		t.Fatalf("create: %d %s", r.status, r.raw)
	}
	if ing := r.body["ingredients"].([]any); ing[1] != "1 onion" {
		t.Fatalf("ingredients not trimmed: %v", ing)
	}
	id := r.body["id"].(string)
	edit := map[string]any{"title": "Red lentil soup", "servings": 6, "ingredients": []string{}, "steps": []string{"Simmer."}, "archived": false, "expectedVersion": 1}
	if r := e.send(adult, "PUT", "/api/recipes/"+id, edit); r.status != 200 || r.body["version"] != float64(2) || r.body["servings"] != float64(6) {
		t.Fatalf("edit: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PUT", "/api/recipes/"+id, edit); r.status != 409 || r.code() != "version_conflict" {
		t.Fatalf("stale edit: %d %s", r.status, r.raw)
	}
	for name, bad := range map[string]map[string]any{
		"blank title":      {"title": "   ", "servings": 1, "ingredients": []string{}, "steps": []string{}},
		"no servings":      {"title": "Soup", "servings": 0, "ingredients": []string{}, "steps": []string{}},
		"blank ingredient": {"title": "Soup", "servings": 1, "ingredients": []string{" "}, "steps": []string{}},
		"long step":        {"title": "Soup", "servings": 1, "ingredients": []string{}, "steps": []string{strings.Repeat("a", 2001)}},
		"41 ingredients":   {"title": "Soup", "servings": 1, "ingredients": make([]string, 41), "steps": []string{}},
	} {
		if r := e.send(adult, "POST", "/api/recipes", bad); r.status != 400 {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	// Archive: hidden from the list unless asked; a display with the meals scope can read it.
	edit["archived"], edit["expectedVersion"] = true, 2
	if r := e.send(adult, "PUT", "/api/recipes/"+id, edit); r.status != 200 || r.body["archived"] != true {
		t.Fatalf("archive: %d %s", r.status, r.raw)
	}
	// The house fixture has one recipe of its own ("Soup").
	if r := e.send(adult, "GET", "/api/recipes", nil); len(r.body["recipes"].([]any)) != 1 {
		t.Fatalf("archived shown: %s", r.raw)
	}
	if r := e.send(adult, "GET", "/api/recipes?archived=true", nil); len(r.body["recipes"].([]any)) != 2 {
		t.Fatalf("archived missing: %s", r.raw)
	}
	if r := e.send(h.actors["display-meals"], "GET", "/api/recipes/"+id, nil); r.status != 200 || len(r.body["steps"].([]any)) != 1 {
		t.Fatalf("display read: %d %s", r.status, r.raw)
	}
	// Another household's recipe is not found.
	other := e.house("b", nil)
	if r := e.send(other.actors["adult"], "GET", "/api/recipes/"+id, nil); r.status != 404 {
		t.Fatalf("cross-household read: %d", r.status)
	}
	if r := e.send(other.actors["adult"], "PUT", "/api/recipes/"+id, edit); r.status != 404 {
		t.Fatalf("cross-household edit: %d", r.status)
	}
}

func TestMealPlan(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult := h.actors["adult"]
	recipe := e.send(adult, "POST", "/api/recipes", map[string]any{"title": "Pancakes", "servings": 4, "ingredients": []string{}, "steps": []string{}}).body["id"].(string)

	// 2026-09-16 is a Wednesday: its week starts Monday 2026-09-14.
	put := func(date, slot string, body map[string]any) resp {
		return e.send(adult, "PUT", "/api/meals/"+date+"/"+slot, body)
	}
	if r := put("2026-09-16", "dinner", map[string]any{"title": "Leftovers", "note": "Finish the soup.", "expectedVersion": 0}); r.status != 200 || r.body["version"] != float64(1) {
		t.Fatalf("plan: %d %s", r.status, r.raw)
	}
	if r := put("2026-09-16", "dinner", map[string]any{"title": "Pizza", "expectedVersion": 0}); r.status != 409 {
		t.Fatalf("overwrite unseen: %d %s", r.status, r.raw)
	}
	if r := put("2026-09-16", "dinner", map[string]any{"recipeId": recipe, "expectedVersion": 1}); r.status != 200 || r.body["title"] != "Pancakes" || r.body["version"] != float64(2) || r.body["note"] != nil {
		t.Fatalf("replace with recipe: %d %s", r.status, r.raw)
	}
	if r := put("2026-09-14", "breakfast", map[string]any{"title": "Porridge", "expectedVersion": 0}); r.status != 200 {
		t.Fatalf("plan Monday: %d", r.status)
	}
	if r := put("2026-09-14", "lunch", map[string]any{"title": "Soup", "recipeId": recipe, "expectedVersion": 0}); r.status != 422 || r.code() != "recipe_or_title" {
		t.Fatalf("both: %d %s", r.status, r.raw)
	}
	if r := put("2026-09-14", "snack", map[string]any{"title": "Fruit", "expectedVersion": 0}); r.status != 400 {
		t.Fatalf("snack: %d", r.status)
	}
	other := e.house("b", nil)
	foreign := e.send(other.actors["adult"], "POST", "/api/recipes", map[string]any{"title": "Theirs", "servings": 1, "ingredients": []string{}, "steps": []string{}}).body["id"].(string)
	if r := put("2026-09-14", "lunch", map[string]any{"recipeId": foreign, "expectedVersion": 0}); r.status != 404 || r.code() != "no_such_recipe" {
		t.Fatalf("other household's recipe: %d %s", r.status, r.raw)
	}

	r := e.send(adult, "GET", "/api/meals?week=2026-09-16", nil)
	if r.status != 200 || r.body["weekStart"] != "2026-09-14" {
		t.Fatalf("week: %d %s", r.status, r.raw)
	}
	days := r.body["days"].([]any)
	if len(days) != 7 || days[6].(map[string]any)["date"] != "2026-09-20" {
		t.Fatalf("days: %s", r.raw)
	}
	wed := days[2].(map[string]any)["meals"].([]any)
	if len(wed) != 1 || wed[0].(map[string]any)["recipeId"] != recipe {
		t.Fatalf("Wednesday: %v", wed)
	}
	// A renamed recipe renames its planned meals.
	e.send(adult, "PUT", "/api/recipes/"+recipe, map[string]any{"title": "Banana pancakes", "servings": 4, "ingredients": []string{}, "steps": []string{}, "archived": false, "expectedVersion": 1})
	r = e.send(adult, "GET", "/api/meals?week=2026-09-20", nil)
	if got := r.body["days"].([]any)[2].(map[string]any)["meals"].([]any)[0].(map[string]any)["title"]; got != "Banana pancakes" {
		t.Fatalf("title after rename: %v", got)
	}
	// Another household sees an empty week; a display needs the meals scope.
	if r := e.send(other.actors["adult"], "GET", "/api/meals?week=2026-09-16", nil); len(r.body["days"].([]any)[2].(map[string]any)["meals"].([]any)) != 0 {
		t.Fatalf("cross-household week: %s", r.raw)
	}
	if r := e.send(h.actors["display-today"], "GET", "/api/meals", nil); r.status != 403 {
		t.Fatalf("display without meals: %d", r.status)
	}

	if r := e.send(adult, "DELETE", "/api/meals/2026-09-16/dinner?expectedVersion=1", nil); r.status != 409 {
		t.Fatalf("stale clear: %d", r.status)
	}
	if r := e.send(adult, "DELETE", "/api/meals/2026-09-16/dinner?expectedVersion=2", nil); r.status != 204 {
		t.Fatalf("clear: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "DELETE", "/api/meals/2026-09-16/dinner?expectedVersion=2", nil); r.status != 404 {
		t.Fatalf("clear again: %d", r.status)
	}
	if r := e.send(adult, "GET", "/api/meals?week=1999-12-31", nil); r.status != 400 {
		t.Fatalf("out of range: %d", r.status)
	}
}

func TestWeekStart(t *testing.T) {
	for in, want := range map[string]string{
		"2026-09-14": "2026-09-14", "2026-09-16": "2026-09-14", "2026-09-20": "2026-09-14", "2026-09-21": "2026-09-21",
		"2027-01-01": "2026-12-28",
	} {
		d, _ := day.ParseDate(in)
		if got := weekStart(d).String(); got != want {
			t.Errorf("weekStart(%s) = %s, want %s", in, got, want)
		}
	}
}
