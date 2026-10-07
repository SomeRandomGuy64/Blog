package scrape

import (
	"slices"
	"testing"
)

func TestTag(t *testing.T) {
	for _, c := range tagCases {
		if got := Tag(c.in, c.strict); got != c.want {
			t.Errorf("Tag(%q, %v) = %q, want %q", c.in, c.strict, got, c.want)
		}
	}
}

func TestTags(t *testing.T) {
	for _, c := range []struct {
		in     []string
		strict bool
		want   []string
	}{
		{[]string{"IMAX with Laser", "Audio Described", "imax", "Wheelchair"}, false, []string{"IMAX"}},
		{[]string{"Subtitled", "HOH Subtitled", "Hindi", "Members Club"}, false, []string{"Subtitled", "Hindi", "Members Club"}},
		{[]string{"Members Club", "Relaxed"}, true, []string{"Relaxed"}},
		{nil, false, []string{}},
	} {
		if got := Tags(c.in, c.strict); !slices.Equal(got, c.want) {
			t.Errorf("Tags(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCamel(t *testing.T) {
	for in, want := range map[string]string{"SubtitledCinema": "Subtitled Cinema", "XPlus": "XPlus", "ScreenX": "Screen X",
		"imax": "imax", "DolbyAtmosSound": "Dolby Atmos Sound", "4DX": "4DX"} {
		if got := Camel(in); got != want {
			t.Errorf("Camel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMins(t *testing.T) {
	for in, want := range map[string]int{"1h 57m": 117, "130 mins": 130, "2 hours": 120, "2hrs 5m": 125, "95 minutes": 95,
		"": 0, "TBC": 0, "1h": 60, "Running time 88 min": 88} {
		if got := Mins(in); got != want {
			t.Errorf("Mins(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestToInt(t *testing.T) {
	for _, c := range []struct {
		in   any
		want int
	}{{nil, 0}, {float64(117), 117}, {117.9, 117}, {"130", 130}, {" 95 ", 95}, {"1h", 0}, {"", 0}, {true, 1}} {
		if got := ToInt(c.in); got != c.want {
			t.Errorf("ToInt(%#v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestShowJSONRoundTrip(t *testing.T) {
	s := Show{Film: "X", Start: "2026-10-07T18:00", Formats: nil}
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"film":"X","start":"2026-10-07T18:00","screen":null,"formats":[],"url":null,"end":null,"duration":null}`
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
	var back Show
	if err := back.UnmarshalJSON([]byte(`{"film":"X","start":"2026-10-07T18:00:00+01:00","screen":"1","formats":["3D"],"url":null,"end":null,"duration":"117"}`)); err != nil {
		t.Fatal(err)
	}
	back.Normalize()
	if back.Start != "2026-10-07T18:00" || back.Screen != "1" || back.Duration != 117 {
		t.Fatalf("got %+v", back)
	}
}
