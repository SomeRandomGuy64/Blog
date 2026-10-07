package ticketing

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Expected values come from running the Python reference (cinemas.py) on the same inputs.

func testMap(t *testing.T, name string, f func(string) string, cases [][2]string) {
	t.Helper()
	for _, c := range cases {
		if got := f(c[0]); got != c[1] {
			t.Errorf("%s(%q) = %q, want %q", name, c[0], got, c[1])
		}
	}
}

func TestCleanTitle(t *testing.T) {
	testMap(t, "cleanTitle", cleanTitle, [][2]string{
		{"NTL: Hamlet (12A)", "NT Live: Hamlet"},
		{"LFF2: Presents: Film X + Q&A with director", "Film X"},
		{"Baby Kino: Dune (15) [Subtitled]", "Dune"},
		{"  Early Doors / Classics: Alien  ", "Alien"},
		{"MLF presents The Thing", "The Thing"},
		{"Relaxed: ", "Relaxed:"},
		{"Opening Night Gala: Hamnet + recorded intro", "Hamnet"},
		{"National Theatre Live : The Seagull", "NT Live: The Seagull"},
		{"EOS: Turner (PG)", "Turner"},
		{"Paris, Texas [35mm]", "Paris, Texas"},
	})
}

func TestTitlecase(t *testing.T) {
	testMap(t, "titlecase", titlecase, [][2]string{
		{"THE LORD OF THE RINGS: THE RETURN OF THE KING", "The Lord of the Rings: The Return of the King"},
		{"ROCKY II", "Rocky II"},
		{"SPIDER-MAN: INTO THE SPIDER-VERSE", "Spider-Man: Into the Spider-Verse"},
		{"AMÉLIE", "Amélie"},
		{"Mixed Case Title", "Mixed Case Title"},
		{"IT CAME FROM OUTER SPACE", "It Came from Outer Space"},
		{"STAR WARS: EPISODE IV, A NEW HOPE", "Star Wars: Episode IV, a New Hope"},
		{"OF MICE AND MEN", "Of Mice and Men"},
		{"LÉON: THE PROFESSIONAL", "Léon: The Professional"},
		{"DR. STRANGELOVE OR: HOW I LEARNED", "Dr. Strangelove Or: How I Learned"},
		{"III", "III"},
		{"MI", "Mi"},
	})
}

func TestVenueTitles(t *testing.T) {
	testMap(t, "bijouTitle", bijouTitle, [][2]string{
		{"AMÉLIE (15, 2001) [SUBTITLED]", "Amélie"},
		{"NT LIVE - HAMLET (12A)", "Hamlet"},
		{"THE GOONIES (PG) - ", "The Goonies"},
		{"ALIEN (2024)", "Alien"},
		{"SPIRITED AWAY (U) [DUBBED] (2001)", "Spirited Away"},
		{"THE ROOM ()", "The Room"},
	})
	testMap(t, "leighTitle", leighTitle, [][2]string{
		{"Fear at the Factory - The Shining (1980)", "The Shining"},
		{"Midweek Matinee: Brief Encounter", "Brief Encounter"},
		{"Stitch &amp; Screen – Mamma Mia!", "Mamma Mia!"},
		{"Leigh Film Society presents... Paris, Texas (1984)", "Paris, Texas"},
		{"Hamlet National Theatre Live at Leigh Film Factory", "Hamlet"},
		{"Nosferatu with live accompaniment by The Band", "Nosferatu"},
		{"Ghostbusters 40th Anniversary", "Ghostbusters"},
		{"Family Film Paddington - Pay As You Feel", "Paddington"},
		{"Up with very Special Guest", "Up"},
	})
	testMap(t, "altTitle", func(s string) string { return altTitleRe.ReplaceAllString(s, "") }, [][2]string{
		{"The Universal Theory [Die Theorie von Allem]", "The Universal Theory"},
		{"Michael (2026)", "Michael"},
		{"Film (a) (b)", "Film (a)"},
	})
	testMap(t, "littleClean", func(s string) string { return littleClean.ReplaceAllString(s, "") }, [][2]string{
		{"La Haine 1995 a film by Mathieu Kassovitz", "La Haine"},
		{"Amelie film three in our French season", "Amelie"},
		{"Plain", "Plain"},
	})
}

func TestFormats(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"NT Live: Hamlet", "", ""}, []string{"Live/Event Cinema"}},
		{[]string{"Amelie [SUBTITLED]", "Parent and Baby screening", "drama"}, []string{"Subtitled", "Parent & Baby"}},
		{[]string{"Metropolis", "with live accompaniment by X", "silent"}, []string{"Silent", "Live Music"}},
		{[]string{"Avatar 3D", "", ""}, []string{"3D"}},
		{[]string{"Grease Sing-a-long", "Q&A with special guest", ""}, []string{"Q&A", "Sing-along"}},
		{[]string{"Relaxed autism-friendly screening of Up", "", ""}, []string{"Relaxed"}},
		{[]string{"Double Bill: Alien 35mm", "", ""}, []string{"35mm", "Double Bill"}},
	}
	for _, c := range cases {
		if got := formats(c.in...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("formats(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSpxFormats(t *testing.T) {
	cases := []struct{ ev, inst string }{
		{`{"name":"Dune: Part Two in 70mm + Q&A","attribute_EventType":"Stage on Screen"}`,
			`{"attribute_Strand":"Silver Screen / Relaxed","attribute_AdditionalEvent":"+ Intro by the curator of the season xx",` +
				`"attribute_Subtitled":true,"attribute_Dubbed":0,"attribute_BSL":"false","attribute_Preview":"",` +
				`"attribute_AudioDescription":"Yes","isOnSale":true}`},
		{`{"name":"ED: Film"}`, `{"attribute_AdditionalEvent":"Q & A with cast"}`},
	}
	want := [][]string{
		{"Relaxed", "Silver Screening", "Intro by the curator of the se", "Subtitled", "Audio Described", "Q&A", "70mm", "Live/Event Cinema"},
		{"Q&A"},
	}
	for i, c := range cases {
		var ev map[string]any
		var inst obj
		if err := json.Unmarshal([]byte(c.ev), &ev); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(c.inst), &inst); err != nil {
			t.Fatal(err)
		}
		if got := spxFormats(ev, inst); !reflect.DeepEqual(got, want[i]) {
			t.Errorf("spxFormats #%d = %q, want %q", i, got, want[i])
		}
	}
}

func TestPeakSubtitled(t *testing.T) {
	page := `<html><body><div><p>7 October 2026</p><h2> The Universal Theory </h2><p>German with English subtitles</p></div>
		<div><p>15 November 2026</p><h2>Michael</h2><p>Biopic</p></div></body></html>`
	got := peakSubtitled([]byte(page))
	if want := map[string]bool{"the universal theory": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("peakSubtitled = %v, want %v", got, want)
	}
}
