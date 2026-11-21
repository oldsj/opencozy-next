package screentime

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Blocked websites. The household keeps two lists, enforced on each child's iPad in every browser
// and by the home router: game portals are always blocked; games only while the child has no iPad
// time running (the router asks the games gate). Browsing is otherwise open. A name covers its
// subdomains on the router, and on the iPad as far as Apple's filter does.

// MaxDomains is the most names on one list.
const MaxDomains = 500

// WebFilter is the household's two lists, each sorted and without repeats.
type WebFilter struct{ GamePortals, Games []string }

var domain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?([.][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// DomainError refuses a list, with a message for adults.
type DomainError struct{ Code, Message string }

func (e DomainError) Error() string { return "screentime: " + e.Message }

// Domains tidies a list as adults type it (spaces, capitals, a trailing dot, repeats) and checks
// every name is a website name like example.com. label names the list in messages.
func Domains(items []string, label string) ([]string, error) {
	out := make([]string, 0, len(items))
	for _, it := range items {
		d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(it)), ".")
		if d == "" {
			continue
		}
		if len(d) > 253 || !domain.MatchString(d) {
			shown := []rune(strings.TrimSpace(it))
			if len(shown) > 80 {
				shown = shown[:80]
			}
			return nil, DomainError{"invalid_domain", fmt.Sprintf("“%s” isn’t a website name like example.com.", string(shown))}
		}
		out = append(out, d)
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) > MaxDomains {
		return nil, DomainError{"too_many_domains", fmt.Sprintf("At most %d %s.", MaxDomains, label)}
	}
	return out, nil
}

// NewWebFilter tidies and checks both lists; a name can't be on both.
func NewWebFilter(gamePortals, games []string) (WebFilter, error) {
	p, err := Domains(gamePortals, "game portals")
	if err != nil {
		return WebFilter{}, err
	}
	g, err := Domains(games, "games")
	if err != nil {
		return WebFilter{}, err
	}
	for _, d := range g {
		if _, found := slices.BinarySearch(p, d); found {
			return WebFilter{}, DomainError{"on_both_lists", d + " is on both lists: a game portal is always blocked."}
		}
	}
	return WebFilter{GamePortals: p, Games: g}, nil
}
