package listings

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

func TestCleanTitleAndKey(t *testing.T) {
	for _, c := range titleCases {
		title, extra := CleanTitle(c.in)
		if title != c.title || !slices.Equal(extra, c.extra) {
			t.Errorf("CleanTitle(%q) = %q %q, want %q %q", c.in, title, extra, c.title, c.extra)
		}
		if k := FilmKey(title); k != c.key {
			t.Errorf("FilmKey(%q) = %q, want %q", title, k, c.key)
		}
	}
}

type rawDump struct {
	From    string          `json:"from"`
	To      string          `json:"to"`
	Results []scrape.Result `json:"results"`
}

func cinemasFor(rs []scrape.Result) []scrape.Cinema {
	chain := map[string]string{"Odeon Oldham": "Odeon", "Vue Bury": "Vue"}
	loc := map[string][2]float64{"Odeon Oldham": {53.5418, -2.1111}, "Vue Bury": {53.59423, -2.29161},
		"Cultplex": {53.49161, -2.23792}, "Showroom Sheffield": {53.37706, -1.46566}}
	var cs []scrape.Cinema
	for _, r := range rs {
		ch := chain[r.Name]
		if ch == "" {
			ch = "Independent"
		}
		cs = append(cs, scrape.Cinema{Name: r.Name, Chain: ch, Lat: loc[r.Name][0], Lng: loc[r.Name][1]})
	}
	return cs
}

// TestBuildMatchesPython replays a synthetic scenario (window, dedupe, title formats, Classic
// rule, runtime medians with the 120 placeholder and half-even rounding, end estimates, title
// votes, errors) whose expected output was produced by the Python build().
func TestBuildMatchesPython(t *testing.T) {
	var raw rawDump
	readJSON(t, "testdata/rules_raw.json", &raw)
	var want Listings
	readJSON(t, "testdata/rules_want.json", &want)
	from, _ := scrape.ParseDate(raw.From)
	to, _ := scrape.ParseDate(raw.To)
	got := Build(cinemasFor(raw.Results), FromResults(raw.Results), from, to, time.Now())

	if len(got.Shows) != len(want.Shows) {
		t.Fatalf("shows: got %d, want %d", len(got.Shows), len(want.Shows))
	}
	for i := range want.Shows {
		g, w := got.Shows[i], want.Shows[i]
		if g.K != w.K || g.C != w.C || g.T != w.T || deref(g.S) != deref(w.S) || !slices.Equal(g.X, w.X) ||
			g.E != w.E || g.D != w.D || g.Es != w.Es {
			t.Errorf("show %d:\n got %+v s=%q\nwant %+v s=%q", i, g, deref(g.S), w, deref(w.S))
		}
	}
	for i, c := range want.Cinemas {
		g := got.Cinemas[i]
		if g.Name != c.Name || g.Count != c.Count || deref(g.Error) != deref(c.Error) || g.Lat != c.Lat || g.Lng != c.Lng {
			t.Errorf("cinema %d: got %+v, want %+v", i, g, c)
		}
	}
	if !mapsEqual(got.Films, want.Films) {
		t.Errorf("films: got %v, want %v", got.Films, want.Films)
	}
	if g, err := time.Parse(time.RFC3339, got.Generated); err != nil || g.IsZero() {
		t.Errorf("generated %q is not RFC 3339", got.Generated)
	}
	if !slices.Equal(got.Premium, want.Premium) {
		t.Errorf("premium: got %v, want %v", got.Premium, want.Premium)
	}
	// (The golden file had Python's Home row/column and "home" keys stripped: the public API omits them.)
	if !slices.EqualFunc(got.Travel.Mins, want.Travel.Mins, slices.Equal) || !slices.Equal(got.Travel.Places, want.Travel.Places) {
		t.Errorf("travel matrix differs:\n got %v\nwant %v", got.Travel.Mins, want.Travel.Mins)
	}
}

func TestBuildUnescapesTitlesAndFlagsStale(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	from := scrape.Day(now)
	cs := []scrape.Cinema{{Name: "Cultplex", Chain: "Independent"}, {Name: "Stockport Plaza", Chain: "Independent"},
		{Name: "Tideswell Cinema", Chain: "Independent"}}
	got := Build(cs, map[string]Entry{
		"Cultplex": {Updated: now.Add(-time.Hour), Shows: []scrape.Show{
			{Film: "Minions &amp; Monsters", Start: "2026-10-07T10:00"}, {Film: "Minions & Monsters", Start: "2026-10-07T13:00"}}},
		"Stockport Plaza": {Updated: now.Add(-13 * time.Hour), Error: "HTTP 503"},
	}, from, from.AddDate(0, 0, 6), now)
	if len(got.Films) != 1 || got.Films["minionsandmonsters"] != "Minions & Monsters" {
		t.Errorf("films = %v", got.Films)
	}
	c := got.Cinemas
	if c[0].Stale || c[0].Updated == nil || *c[0].Updated != "2026-10-07T12:00:00+01:00" {
		t.Errorf("fresh cinema: %+v", c[0])
	}
	if !c[1].Stale || deref(c[1].Error) != "HTTP 503" {
		t.Errorf("stale cinema: %+v", c[1])
	}
	if !c[2].Stale || c[2].Updated != nil || deref(c[2].Error) != "not fetched yet" {
		t.Errorf("missing cinema: %+v", c[2])
	}
}

func TestEstimateMinutes(t *testing.T) {
	// Values from travel.estimate_minutes(travel.haversine_km(...)).
	// Odeon Manchester Great Northern -> HOME Manchester, Odeon Liverpool ONE -> City Screen York.
	if got := EstimateMinutes(HaversineKm(53.4773, -2.249, 53.47293, -2.2466)); got != 9 {
		t.Errorf("GN->HOME = %d, want 9", got)
	}
	if got := EstimateMinutes(HaversineKm(53.4043, -2.9868, 53.9591, -1.0848)); got != 210 {
		t.Errorf("Liverpool ONE->York = %d, want 210", got)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestCinemaAdMinutes(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	from := scrape.Day(now)
	cs := []scrape.Cinema{{Name: "HOME Manchester", Chain: "Independent"}, {Name: "Hebden Bridge Picture House", Chain: "Independent"},
		{Name: "Cultplex", Chain: "Independent"}}
	got := Build(cs, map[string]Entry{
		// HOME's slot (12:40-15:13) is ignored for start + 15 + runtime; without a runtime the slot still gives it
		"HOME Manchester": {Updated: now, Shows: []scrape.Show{{Film: "Uprising", Start: "2026-10-20T12:40", End: "2026-10-20T15:13", Duration: 128},
			{Film: "Deadline", Start: "2026-10-20T13:00", End: "2026-10-20T15:15"}}},
		"Hebden Bridge Picture House": {Updated: now, Shows: []scrape.Show{{Film: "Just an Illusion", Start: "2026-10-20T12:30", Duration: 119}}},
		"Cultplex":                    {Updated: now, Shows: []scrape.Show{{Film: "Derek vs Derek", Start: "2026-10-20T14:00", Duration: 88}}},
	}, from, from.AddDate(0, 0, 6), now)
	want := map[string][2]any{"uprising": {"2026-10-20T15:03", 128}, "deadline": {"2026-10-20T15:15", 120},
		"justanillusion": {"2026-10-20T14:59", 119}, "derekvsderek": {"2026-10-20T15:38", 88}}
	for _, s := range got.Shows {
		if w := want[s.K]; s.E != w[0] || s.D != w[1] {
			t.Errorf("%s: end %s runtime %d, want %v", s.K, s.E, s.D, w)
		}
	}
	for i, ads := range []int{15, 30, 10} {
		if got.Cinemas[i].Ads != ads {
			t.Errorf("%s ads = %d, want %d", got.Cinemas[i].Name, got.Cinemas[i].Ads, ads)
		}
	}
}
