package scrape

import (
	"context"
	"testing"
	"time"
)

// The home-connection guarantee: no caller can scrape a relay cinema past DefaultDays.
func TestRelayCinemasNeverPassDefaultDays(t *testing.T) {
	if DefaultDays != 16 {
		t.Fatalf("DefaultDays = %d: raising it adds requests from the home connection", DefaultDays)
	}
	from := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	got := map[string]time.Time{}
	fetch := func(name string) Fetcher {
		return func(_ *Session, _, to time.Time) ([]Show, error) {
			got[name] = to
			return nil, nil
		}
	}
	to := from.AddDate(0, 0, ServerDays-1)
	Run(context.Background(), []Cinema{{Name: "relay", Relay: true, Fetch: fetch("relay")}}, from, to, Options{})
	Run(context.Background(), []Cinema{{Name: "server", Fetch: fetch("server")}}, from, to, Options{})
	if want := from.AddDate(0, 0, DefaultDays-1); !got["relay"].Equal(want) {
		t.Errorf("relay cinema scraped to %s, want %s", got["relay"], want)
	}
	if !got["server"].Equal(to) {
		t.Errorf("server cinema scraped to %s, want %s", got["server"], to)
	}
}

func TestDailyTo(t *testing.T) {
	from := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	if short := from.AddDate(0, 0, 3); !DailyTo(from, short).Equal(short) {
		t.Error("a window under the cap must not change")
	}
	if got, want := DailyTo(from, from.AddDate(0, 0, 55)), from.AddDate(0, 0, 15); !got.Equal(want) {
		t.Errorf("DailyTo = %s, want %s", got, want)
	}
}
