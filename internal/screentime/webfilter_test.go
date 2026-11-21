package screentime

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestNewWebFilter(t *testing.T) {
	f, err := NewWebFilter([]string{" Games.Example.test. ", "games.example.test", "", "portal.example.test"}, []string{"b.example.test", "a.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.GamePortals, []string{"games.example.test", "portal.example.test"}) || !slices.Equal(f.Games, []string{"a.example.test", "b.example.test"}) {
		t.Errorf("tidied: %+v", f)
	}
	if f, err := NewWebFilter(nil, nil); err != nil || len(f.GamePortals) != 0 || len(f.Games) != 0 {
		t.Errorf("empty: %+v %v", f, err)
	}

	many := make([]string, MaxDomains+1)
	for i := range many {
		many[i] = fmt.Sprintf("site%d.example.test", i)
	}
	for _, c := range []struct {
		portals, games []string
		code           string
	}{
		{[]string{"localhost"}, nil, "invalid_domain"},
		{nil, []string{"https://example.test"}, "invalid_domain"},
		{nil, []string{"-bad.example.test"}, "invalid_domain"},
		{nil, []string{strings.Repeat("a", 64) + ".test"}, "invalid_domain"},
		{many, nil, "too_many_domains"},
		{[]string{"x.example.test"}, []string{"X.example.test"}, "on_both_lists"},
	} {
		var de DomainError
		if _, err := NewWebFilter(c.portals, c.games); !errors.As(err, &de) || de.Code != c.code {
			t.Errorf("%v %v: %v, want %s", c.portals, c.games, err, c.code)
		}
	}
	if _, err := NewWebFilter(many[:MaxDomains], nil); err != nil {
		t.Errorf("exactly %d: %v", MaxDomains, err)
	}
}
