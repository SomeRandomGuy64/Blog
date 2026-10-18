package registry

import (
	"slices"
	"testing"

	"fazon.nexus/cinema/internal/region"
	"fazon.nexus/cinema/internal/scrape"
)

func TestRegistry(t *testing.T) {
	seen, chains := map[string]bool{}, map[string]int{}
	for _, c := range Cinemas {
		if seen[c.Name] {
			t.Errorf("duplicate %q", c.Name)
		}
		seen[c.Name] = true
		if c.Fetch == nil || c.URL == "" || c.Chain == "" {
			t.Errorf("incomplete entry %+v", c)
		}
		if c.Lat < 49.8 || c.Lat > 60.9 || c.Lng < -8.7 || c.Lng > 1.8 {
			t.Errorf("%q is located outside the UK (%v, %v)", c.Name, c.Lat, c.Lng)
		}
		if !region.Valid(c.Region) {
			t.Errorf("%q has region %q", c.Name, c.Region)
		}
		if c.Relay != (relayChains[c.Chain] || c.Name == "Plaza Cinema Skipton") {
			t.Errorf("%q: relay %v", c.Name, c.Relay)
		}
		chains[c.Chain]++
	}
	if chains["Independent"] != len(Places) {
		t.Errorf("%d independents, %d places", chains["Independent"], len(Places))
	}
	for _, ch := range []string{"Odeon", "Curzon", "Cineworld", "Everyman", "Showcase", "Vue", "Picturehouse", "The Light", "Reel"} {
		if chains[ch] == 0 {
			t.Errorf("no %s cinemas (run `cinema discover`)", ch)
		}
	}
	// The original Greater Manchester / Yorkshire set keeps its names (the store and saved settings use them).
	for _, n := range []string{"Odeon Liverpool ONE", "Vue MediaCity", "Cineworld Ashton", "Showcase Leeds",
		"Picturehouse at FACT Liverpool", "Everyman Manchester St Johns", "The Light New Brighton", "Reel Rochdale", "Curzon Knutsford"} {
		if !seen[n] {
			t.Errorf("missing %q", n)
		}
	}
}

func TestSelect(t *testing.T) {
	yes, no := true, false
	if n := len(Select("", &yes)) + len(Select("", &no)); n != len(Cinemas) {
		t.Errorf("relay split covers %d of %d", n, len(Cinemas))
	}
	if got := Select("rochdale", &yes); len(got) != 1 || got[0].Name != "Odeon Rochdale" {
		t.Errorf("relay rochdale -> %v, want Odeon only", got)
	}
}

func TestInRegions(t *testing.T) {
	nw := InRegions([]string{"north-west"})
	if len(nw) == 0 || !slices.ContainsFunc(nw, func(c scrape.Cinema) bool { return c.Name == "HOME Manchester" }) {
		t.Errorf("north-west: %d cinemas, HOME missing", len(nw))
	}
	total := 0
	for _, r := range region.All {
		total += len(InRegions([]string{r.Key}))
	}
	if total != len(Cinemas) {
		t.Errorf("regions cover %d of %d cinemas", total, len(Cinemas))
	}
}
