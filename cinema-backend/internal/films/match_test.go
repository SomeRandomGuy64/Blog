package films

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBaseMerges(t *testing.T) {
	groups := map[string][]string{
		"donniedarko": {"Donnie Darko", "Donnie Darko (25th Anniversary)", "Donnie Darko 4K (25th Anniversary)",
			"LMF Presents: Donnie Darko 4K (25th Anniversary)", "Throwback: Donnie Darko (25th Anniversary)"},
		"rocky": {"Rocky", "Rocky (50th Anniversary)", "Rocky 50th Anniversary 4K", "Rocky (50th Anniversary 4K Restoration)", "Throwback: Rocky (50th Anniversary)"},
		"panslabyrinth": {"Pan's Labyrinth", "Pan's Labyrinth (20th Anniversary)", "Pan's Labyrinth (20th Anniversary 4K Restoration)",
			"Pan's Labyrinth (20th Anniversary Restoration)"},
		"scream":                         {"Scream (30th Anniversary)", "SCREAM (30th Anniversary, 4K Restoration)"},
		"holiday":                        {"Throwback: The Holiday", "Throwback : The Holiday"},
		"itsawonderfullife":              {"It's A Wonderful Life", "Throwback: It's a Wonderful Life (80th Anniversary)"},
		"marieantoinette":                {"Marie Antoinette", "Marie Antoinette 20th Anniversary", "Sunday Cinema Club Presents: Marie Antoinette"},
		"deadsouls":                      {"Dead Souls (CTBA)", "Dead Souls", "Dead Souls + Q&A"},
		"santaclausthemovie":             {"Santa Claus The Movie (Cert PG)", "Santa Claus: The Movie"},
		"dunepart3":                      {"Dune: Part 3 (Preview)", "Dune: Part 3", "Dune: Part Three"},
		"irregularatmagichighschool":     {"The Irregular at Magic High School: Yotsuba", "The Irregular at Magic High School : Yotsuba"},
		"jakzyczebyniezwariowac":         {"Jak żyć, żeby nie zwariować", "Jak zyc, zeby nie zwariowac"},
		"andrerieus2026christmasconcert": {"André Rieu's 2026 Christmas Concert", "Andre Rieu's 2026 Christmas Concert"},
		"dracula":                        {"Dracula", "DRACULA 4K RESTORATION"},
		"peterasher":                     {"Peter Asher", "Peter Asher + Q&A"},
		"cottonqueen":                    {"Cotton Queen", "Cotton Queen + director Q&A"},
	}
	for want, titles := range groups {
		for _, title := range titles {
			b, _ := Base(title)
			if k := Key(b); !strings.HasPrefix(k, want) && k != want {
				t.Errorf("%q: key %q, want %q", title, k, want)
			}
			if first, _ := Base(titles[0]); Key(b) != Key(first) {
				t.Errorf("%q (%q) and %q (%q) should share a key", title, Key(b), titles[0], Key(first))
			}
		}
	}
}

func TestBaseKeepsApart(t *testing.T) {
	apart := [][2]string{
		{"Ramayana (Hindi)", "Ramayana (Tamil)"}, {"Ramayana", "Ramayana (Telugu)"},
		{"Fatherland", "Fatherland (German language)"},
		{"Donnie Darko", "Donnie Darko (& Dummy Run)"},
		{"Shaun the Sheep", "Shaun the Sheep + draw-along"},
		{"Rocky", "Rocky II"}, {"Dune: Part 2", "Dune: Part 3"},
		{"Kids Club: Fantastic Mr Fox", "Fantastic Mr Fox"},
	}
	for _, p := range apart {
		a, _ := Base(p[0])
		b, _ := Base(p[1])
		if Key(a) == Key(b) {
			t.Errorf("%q and %q merged", p[0], p[1])
		}
	}
	for _, title := range []string{"The Anniversary", "The Restoration", "Preview", "Restoration", "4K", "Anniversary"} {
		if b, _ := Base(title); b != title {
			t.Errorf("Base(%q) = %q", title, b)
		}
	}
}

func TestBaseTags(t *testing.T) {
	for title, want := range map[string][]string{
		"Throwback: Rocky (50th Anniversary 4K)": {"Classic", "4K"},
		"Peter Asher + Q&A":                      {"Q&A"},
		"Dune: Part 3 (Preview)":                 {"Preview"},
		"Rocky":                                  nil,
	} {
		_, got := Base(title)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%q tags %v, want %v", title, got, want)
		}
	}
}

func TestSimilar(t *testing.T) {
	yes := [][2]string{{"Kids Club: Fantastic Mr Fox", "Fantastic Mr Fox"}, {"Ramayana (Hindi)", "Ramayana"},
		{"Fatherland (German language)", "Fatherland"}, {"Jak Zyc Zeby Nie Zwariowac", "Jak Zyc Zeby Nie Zwarjowac"}}
	no := [][2]string{{"Dune", "Dune: Part 3"}, {"Wicked", "Wicked: For Good"}, {"Rocky", "Rocky II"},
		{"Toy Story 3", "Toy Story 4"}, {"Avatar", "Avatar: Fire and Ash"}, {"Ramayana (Hindi)", "Ramayana (Tamil)"}, {"Elf", "Elf Kids Club"},
		{"SHAFF Retrospective: Saturday - Block A", "SHAFF Retrospective: Saturday - Block B"}, {"George Michael: The Faith Tour", "Michael"},
		{"PBPB: Primal Scream - Screamadelica", "Scream"}}
	for _, p := range yes {
		if !Similar(p[0], p[1]) {
			t.Errorf("%q / %q should be similar", p[0], p[1])
		}
	}
	for _, p := range no {
		if Similar(p[0], p[1]) {
			t.Errorf("%q / %q should not be similar", p[0], p[1])
		}
	}
}

func TestLanguageOnly(t *testing.T) {
	if !LanguageOnly("Jailer 2", "Jailer 2 (Tamil)") || !LanguageOnly("Fatherland (German language)", "Fatherland") {
		t.Error("language labels")
	}
	if LanguageOnly("Golden Boy", "NT Live: Golden Boy") {
		t.Error("a strand is not a language")
	}
}

func TestStore(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Merge("a", "b"); err != nil {
		t.Fatal(err)
	}
	if err := st.Merge("b", "c"); err != nil {
		t.Fatal(err)
	}
	if err := st.Merge("c", "a"); err != ErrCycle {
		t.Errorf("cycle: %v", err)
	}
	if err := st.Merge("a", "a"); err != ErrCycle {
		t.Errorf("self: %v", err)
	}
	if err := st.Merge("A!", "b"); err != ErrKey {
		t.Errorf("bad key: %v", err)
	}
	st.Apart("y", "x")
	st.Split("z")
	st.PutLookup("seen", Lookup{ID: 7, Checked: time.Now()})
	st.PutLookup("old", Lookup{Checked: time.Now().Add(-2 * RecheckAfter)})
	st.PutLookup("recent", Lookup{Checked: time.Now()})
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := again.Snapshot()
	if s.Merge["a"] != "b" || s.Merge["z"] != "z" || !s.Apart[PairKey("x", "y")] {
		t.Errorf("reload: %+v", s)
	}
	got := again.Missing(map[string]string{"seen": "", "old": "", "recent": "", "new": ""}, time.Now())
	if !slices.Equal(got, []string{"new", "old"}) {
		t.Errorf("missing: %v", got)
	}
}

func TestTMDB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"results":[{"id":1,"title":"Rocky Balboa","release_date":"2006-12-20"},{"id":2,"title":"Rocky","release_date":"1976-11-21"}]}`))
	}))
	defer srv.Close()
	db := &TMDB{Key: "k", Base: srv.URL}
	l, err := db.Lookup(context.Background(), "Rocky", time.Now())
	if err != nil || l.ID != 2 || l.Year != "1976" || l.Key != "rocky" {
		t.Errorf("exact: %+v %v", l, err)
	}
	if l, err := db.Lookup(context.Background(), "Nothing Like It", time.Now()); err != nil || l.ID != 0 {
		t.Errorf("no match: %+v %v", l, err)
	}
	bad := &TMDB{Key: "wrong", Base: srv.URL}
	if _, err := bad.Lookup(context.Background(), "Rocky", time.Now()); err == nil || strings.Contains(err.Error(), "wrong") {
		t.Errorf("error must not hold the key: %v", err)
	}
}
