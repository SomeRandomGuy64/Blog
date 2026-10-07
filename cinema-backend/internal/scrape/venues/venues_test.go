package venues

import (
	"slices"
	"testing"
	"time"
)

// Expected values come from running the Python helpers in cinemas.py.

func TestSvFormats(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want []string
	}{
		{[]string{"Subtitled &amp; HOH", "Drama"}, []string{"Subtitled"}},
		{[]string{"NT Live: Hamlet"}, []string{"Live/Event Cinema"}},
		{[]string{"Parent &amp; Baby screening", "3D"}, []string{"3D", "Parent & Baby"}},
		{[]string{"Q &amp; A with director", "35 mm print"}, []string{"35mm", "Q&A"}},
		{[]string{"Audio-Described", "Dementia friendly"}, []string{"Audio Described", "Relaxed"}},
		{[]string{"royal ballet", "70mm"}, []string{"70mm", "Live/Event Cinema"}},
		{[]string{"Carers and babies"}, []string{"Parent & Baby"}},
		{[]string{"Exhibition on Screen: Monet"}, []string{"Live/Event Cinema"}},
		{[]string{"q and a"}, []string{"Q&A"}},
		{[]string{"plain film"}, []string{}},
		{[]string{"captioned relaxed"}, []string{"Subtitled", "Relaxed"}},
	} {
		if got := svFormats(c.in...); !slices.Equal(got, c.want) {
			t.Errorf("svFormats(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFmtTags(t *testing.T) {
	for in, want := range map[string][]string{
		"IMAX 3D: Avatar | IMAX 3D":  {"IMAX", "3D"},
		"Film | Event cinema":        {"Live/Event Cinema"},
		"Babies screening":           {"Parent & Baby"},
		"Q&A with cast":              {"Q&A"},
		"Live from the Met":          {"Live/Event Cinema"},
		"National Theatre (Live)":    {"Live/Event Cinema"},
		"Subs: HOH":                  {"Subtitled"},
		"Audio-described relaxed":    {"Audio Described", "Relaxed"},
		"Parent and baby":            {"Parent & Baby"},
		"Introduced by the director": {"Q&A"},
		"35mm presentation":          {"35mm"},
		"Encore: Swan Lake":          {"Live/Event Cinema"},
		"Live: Coldplay":             {"Live/Event Cinema"},
		"Film | Film":                {},
	} {
		if got := fmtTags(in); !slices.Equal(got, want) {
			t.Errorf("fmtTags(%q) = %q, want %q", in, got, want)
		}
	}
}

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNearestYear(t *testing.T) {
	for _, c := range []struct {
		m        time.Month
		day      int
		ref, out string
	}{
		{10, 8, "2026-10-07", "2026-10-08"}, {8, 9, "2026-10-07", "2026-08-09"}, {8, 7, "2026-10-07", "2027-08-07"},
		{1, 5, "2026-10-07", "2027-01-05"}, {2, 29, "2026-10-07", "2028-02-29"}, {12, 25, "2026-01-03", "2025-12-25"},
		{10, 1, "2026-12-31", "2027-10-01"},
	} {
		got, err := nearestYear(c.m, c.day, d(c.ref))
		if err != nil || !got.Equal(d(c.out)) {
			t.Errorf("nearestYear(%d, %d, %s) = %v, %v; want %s", c.m, c.day, c.ref, got, err, c.out)
		}
	}
	if _, err := nearestYear(2, 31, d("2026-10-07")); err == nil {
		t.Error("nearestYear(2, 31) should fail")
	}
}

func TestGuessYear(t *testing.T) {
	ref := d("2026-10-07")
	for _, c := range []struct {
		day       int
		m         time.Month
		wday, out string
	}{
		{26, 4, "monday", "2027-04-26"}, {26, 4, "", "2027-04-26"}, {8, 10, "thursday", "2026-10-08"},
		{8, 10, "wednesday", "2026-10-08"}, {29, 2, "", "2028-02-29"}, {5, 1, "tuesday", "2027-01-05"},
		{9, 8, "sunday", "2026-08-09"}, {5, 1, "wednesday", "2028-01-05"}, {9, 8, "saturday", "2026-08-09"},
	} {
		got, err := guessYear(c.day, c.m, c.wday, ref)
		if err != nil || !got.Equal(d(c.out)) {
			t.Errorf("guessYear(%d, %d, %q) = %v, %v; want %s", c.day, c.m, c.wday, got, err, c.out)
		}
	}
}

func TestSpFilm(t *testing.T) {
	for in, want := range map[string]string{
		"Classic Film: Casablanca":          "Casablanca",
		"Film - Jaws":                       "Jaws",
		"Family Film: Paddington":           "Paddington",
		"Film: Heat":                        "Heat",
		"Silent Film Night with live organ": "Silent Film Night with live organ",
		"The Film Club":                     "The Film Club",
		"Kids' Film - Moana 2":              "Moana 2",
		"Casablanca":                        "Casablanca",
	} {
		if got := spFilm(in); got != want {
			t.Errorf("spFilm(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMonthAbbr(t *testing.T) {
	if m, err := monthAbbr("oct"); err != nil || m != time.October {
		t.Errorf("monthAbbr(oct) = %v, %v", m, err)
	}
	if _, err := monthAbbr("Thu"); err == nil {
		t.Error("monthAbbr(Thu) should fail")
	}
}
