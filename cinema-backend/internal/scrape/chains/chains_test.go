package chains

import (
	"slices"
	"testing"
)

func TestWebediaTags(t *testing.T) {
	// From the Python fetch_webedia tag loop.
	got := webediaTags([]string{"Showtime.Format.Projection.IMAX", "Internal.Something", "Internal.XPlus",
		"Auditorium.Comfort.Recliner", "Showtime.Subtitle.HardOfHearing", "Showtime.Accessibility.AudioDescribed", "Solo"})
	want := []string{"IMAX", "XPlus", "Hard Of Hearing subs", "Audio Described", "Solo"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWebediaScreen(t *testing.T) {
	for in, want := range map[string]string{"7": "Screen 7", "IMAX": "IMAX", "": ""} {
		sh := webediaShow{}
		if in != "" {
			sh.Screen = &struct {
				Name string `json:"name"`
			}{in}
		}
		if got := webediaScreen(sh); got != want {
			t.Errorf("webediaScreen(%q) = %q, want %q", in, got, want)
		}
	}
}
