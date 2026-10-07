package registry

import (
	"testing"

	"fazon.nexus/cinema/internal/listings"
)

func TestRegistry(t *testing.T) {
	if len(Cinemas) != 57 {
		t.Errorf("%d cinemas, want 57", len(Cinemas))
	}
	seen, relay := map[string]bool{}, 0
	for _, c := range Cinemas {
		if seen[c.Name] {
			t.Errorf("duplicate %q", c.Name)
		}
		seen[c.Name] = true
		if c.Fetch == nil || c.URL == "" || c.Chain == "" {
			t.Errorf("incomplete entry %+v", c)
		}
		if _, ok := listings.Places[c.Name]; !ok {
			t.Errorf("%q has no location in listings.Places", c.Name)
		}
		if c.Relay {
			relay++
		}
	}
	if relay != 17 {
		t.Errorf("%d relay cinemas, want 17", relay)
	}
}

func TestSelect(t *testing.T) {
	yes, no := true, false
	if n := len(Select("odeon,vue", nil)); n != 11 {
		t.Errorf("odeon,vue -> %d, want 11", n)
	}
	if n := len(Select("", &yes)) + len(Select("", &no)); n != len(Cinemas) {
		t.Errorf("relay split covers %d of %d", n, len(Cinemas))
	}
	if n := len(Select("rochdale", &yes)); n != 1 {
		t.Errorf("relay rochdale -> %d, want 1 (Odeon only)", n)
	}
}
