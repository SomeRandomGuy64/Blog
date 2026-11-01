package listings

import (
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"fazon.nexus/cinema/internal/films"
	"fazon.nexus/cinema/internal/scrape"
)

func emptyMatches() *films.Snapshot {
	return &films.Snapshot{Merge: map[string]string{}, Apart: map[string]bool{}, Confirmed: map[string]bool{}, Lookups: map[string]films.Lookup{}}
}

// matched builds listings from "cinema|title|runtime" lines, each a showing a day apart.
func matched(t *testing.T, m *films.Snapshot, lines ...[3]any) *Listings {
	t.Helper()
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	entries := map[string]Entry{}
	var cinemas []scrape.Cinema
	for i, l := range lines {
		name := l[0].(string)
		if _, ok := entries[name]; !ok {
			cinemas = append(cinemas, scrape.Cinema{Name: name, Chain: "Independent"})
		}
		e := entries[name]
		e.Shows = append(e.Shows, scrape.Show{Film: l[1].(string), Duration: l[2].(int),
			Start: from.AddDate(0, 0, i%5).Add(time.Duration(10+i%8) * time.Hour).Format(scrape.TimeFmt)})
		entries[name] = e
	}
	return BuildMatched(cinemas, entries, from, from.AddDate(0, 0, 9), from, m)
}

func filmKeys(l *Listings) []string { return slices.Sorted(maps.Keys(l.Films)) }

func TestRulesMergeAndName(t *testing.T) {
	l := matched(t, emptyMatches(),
		[3]any{"A", "Throwback: Rocky (50th Anniversary)", 120}, [3]any{"B", "Rocky", 119}, [3]any{"B", "Rocky", 119}, [3]any{"C", "Rocky 50th Anniversary 4K", 121})
	if got := filmKeys(l); !slices.Equal(got, []string{"rocky"}) {
		t.Fatalf("films %v", got)
	}
	if l.Films["rocky"] != "Rocky" {
		t.Errorf("title %q", l.Films["rocky"])
	}
	n := l.Notes["rocky"]
	if !n.U || len(n.Names) != 3 {
		t.Errorf("note %+v", n)
	}
	if l.Aliases["throwbackrocky50thanniversary"] != "rocky" {
		t.Errorf("aliases %v", l.Aliases)
	}
	has4K := false
	for _, s := range l.Shows {
		has4K = has4K || slices.Contains(s.X, "4K")
		if s.K != "rocky" {
			t.Errorf("show key %q", s.K)
		}
	}
	if !has4K {
		t.Error("4K tag lost")
	}
}

func TestNoMatchingLeavesKeysAlone(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	entries := map[string]Entry{"A": {Shows: []scrape.Show{{Film: "Throwback: Rocky", Start: "2026-11-01T12:00"}}}}
	l := Build([]scrape.Cinema{{Name: "A", Chain: "Independent"}}, entries, from, from.AddDate(0, 0, 3), from)
	if _, ok := l.Films["throwbackrocky"]; !ok || l.Notes != nil || l.Aliases != nil {
		t.Errorf("%v %v %v", l.Films, l.Notes, l.Aliases)
	}
}

func TestMergedShowingIsDedupedAtOneCinema(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	entries := map[string]Entry{"A": {Shows: []scrape.Show{{Film: "Elf", Start: "2026-11-01T12:00"}, {Film: "Throwback: Elf", Start: "2026-11-01T12:00"}}}}
	l := BuildMatched([]scrape.Cinema{{Name: "A", Chain: "Independent"}}, entries, from, from.AddDate(0, 0, 3), from, emptyMatches())
	if len(l.Shows) != 2 { // the Classic tag differs, so these stay two rows of one film
		t.Logf("shows %d", len(l.Shows))
	}
	if got := filmKeys(l); !slices.Equal(got, []string{"elf"}) {
		t.Errorf("films %v", got)
	}
}

func TestRuntimeGuard(t *testing.T) {
	l := matched(t, emptyMatches(),
		[3]any{"A", "Pressure", 106}, [3]any{"A", "Pressure", 106}, [3]any{"B", "Pressure (20th Anniversary)", 170})
	if got := filmKeys(l); len(got) != 2 {
		t.Fatalf("runtimes 64 minutes apart merged: %v", got)
	}
	if n := l.Notes["pressure"]; len(n.S) != 1 || !n.U {
		t.Errorf("should still be suggested: %+v", n)
	}
	// A placeholder 120 is no evidence either way.
	l = matched(t, emptyMatches(), [3]any{"A", "Digger", 129}, [3]any{"B", "Digger (10th Anniversary)", 120})
	if got := filmKeys(l); len(got) != 1 {
		t.Errorf("placeholder runtime split a film: %v", got)
	}
}

func TestRuntimeGuardWhenPlainKeyIsTheOddOneOut(t *testing.T) {
	l := matched(t, emptyMatches(), [3]any{"A", "Pressure", 90}, [3]any{"B", "Throwback: Pressure", 170},
		[3]any{"B", "Throwback: Pressure", 170}, [3]any{"C", "Pressure (20th Anniversary)", 171})
	if got := filmKeys(l); len(got) != 2 {
		t.Fatalf("films %v", got)
	}
	if l.Films["pressure"] != "Pressure" {
		t.Errorf("the plain listing lost its key: %v", l.Films)
	}
}

func TestOwnerDecisions(t *testing.T) {
	lines := [][3]any{{"A", "Kids Club: Home Alone", 97}, {"B", "Home Alone", 97}, {"C", "Throwback: Home Alone", 97}}
	m := emptyMatches()
	l := matched(t, m, lines...)
	if len(l.Notes["homealone"].S) != 1 || !l.Notes["homealone"].U {
		t.Fatalf("suggestion %+v", l.Notes)
	}
	m.Merge["kidsclubhomealone"] = "homealone"
	l = matched(t, m, lines...)
	if got := filmKeys(l); !slices.Equal(got, []string{"homealone"}) {
		t.Fatalf("merge: %v", got)
	}
	if n := l.Notes["homealone"]; n.U || len(n.Names) != 3 {
		t.Errorf("an owner merge is confirmed: %+v", n)
	}

	m = emptyMatches()
	m.Merge["throwbackhomealone"] = "throwbackhomealone"
	l = matched(t, m, lines...)
	if got := filmKeys(l); !slices.Contains(got, "throwbackhomealone") {
		t.Errorf("split: %v", got)
	}

	m = emptyMatches()
	m.Apart[films.PairKey("homealone", "kidsclubhomealone")] = true
	m.Confirmed["homealone"] = true
	if n := matched(t, m, lines...).Notes["homealone"]; n.U || len(n.S) != 0 {
		t.Errorf("apart + confirmed: %+v", n)
	}

	// Merging a whole film moves all its listings, and a loop can't hang the build.
	m = emptyMatches()
	m.Merge["homealone"] = "kidsclubhomealone"
	m.Merge["kidsclubhomealone"] = "homealone"
	matched(t, m, lines...)
	m = emptyMatches()
	m.Merge["homealone"] = "kidsclubhomealone"
	if got := filmKeys(matched(t, m, lines...)); !slices.Equal(got, []string{"kidsclubhomealone"}) {
		t.Errorf("whole film: %v", got)
	}
}

func TestDatabaseConfirmsAndCanJoinTitles(t *testing.T) {
	m := emptyMatches()
	m.Lookups["spiritedaway"] = films.Lookup{ID: 129, Title: "Spirited Away", Key: "spiritedaway"}
	m.Lookups["senandchihiro"] = films.Lookup{ID: 129, Title: "Spirited Away", Key: "spiritedaway"}
	l := matched(t, m, [3]any{"A", "Spirited Away", 125}, [3]any{"B", "Sen and Chihiro", 125})
	if got := filmKeys(l); !slices.Equal(got, []string{"spiritedaway"}) {
		t.Fatalf("films %v", got)
	}
	if n := l.Notes["spiritedaway"]; n.U {
		t.Errorf("database match shouldn't be badged: %+v", n)
	}
}

func TestSequelsStayApart(t *testing.T) {
	var lines [][3]any
	for i, title := range []string{"Dune", "Dune: Part 2", "Dune: Part 3", "Rocky", "Rocky II", "Toy Story 3", "Toy Story 4"} {
		lines = append(lines, [3]any{fmt.Sprint("C", i), title, 150})
	}
	l := matched(t, emptyMatches(), lines...)
	if len(l.Films) != 7 || len(l.Notes) != 0 {
		t.Errorf("films %v notes %v", l.Films, l.Notes)
	}
}
