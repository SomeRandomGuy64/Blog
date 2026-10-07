package store

import (
	"testing"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

func TestApplyKeepsLastGood(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	good := scrape.Result{Name: "Cultplex", Fetched: t0, Shows: []scrape.Show{{Film: "X", Start: "2026-10-07T19:30"}}}
	if _, err := s.Apply(good, "server"); err != nil {
		t.Fatal(err)
	}
	_, gen1 := s.Entries()
	if _, err := s.Apply(scrape.Result{Name: "Cultplex", Fetched: t0.Add(2 * time.Hour), Error: "HTTP 503"}, "server"); err != nil {
		t.Fatal(err)
	}
	// An older upload must not overwrite newer state.
	if _, err := s.Apply(scrape.Result{Name: "Cultplex", Fetched: t0.Add(-time.Hour)}, "relay"); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dir) // survives a restart
	if err != nil {
		t.Fatal(err)
	}
	e, gen2 := s2.Entries()
	r := e["Cultplex"]
	if len(r.Shows) != 1 || r.Error != "HTTP 503" || !r.Updated.Equal(t0) {
		t.Fatalf("got %+v", r)
	}
	if _, g := s.Entries(); g == gen1 {
		t.Error("generation did not change")
	}
	_ = gen2

	if err := s2.Prune(func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if s3, _ := Open(dir); len(s3.rec) != 0 {
		t.Errorf("prune left %d records", len(s3.rec))
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"Odeon Liverpool ONE": "odeon-liverpool-one", "HOME Manchester": "home-manchester",
		"Picturehouse at FACT Liverpool": "picturehouse-at-fact-liverpool", "The Light New Brighton!": "the-light-new-brighton"} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
