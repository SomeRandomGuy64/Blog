package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"fazon.nexus/cinema/internal/scrape"
	"fazon.nexus/cinema/internal/store"
)

// Tiered scraping: a cinema in a "hot" region (one of the always-hot defaults, or one somebody
// has viewed within hotFor) is refreshed every hot interval; the rest of the country only every
// coldEvery, a capped batch at a time, so going nationwide doesn't multiply the request volume.

// demand remembers when each region was last requested, persisted so a restart doesn't
// demote everything to cold.
type demand struct {
	path   string
	always []string
	hotFor time.Duration
	kick   chan struct{} // a region turned hot: scrape it now rather than at the next tick

	mu    sync.Mutex
	last  map[string]time.Time
	dirty bool
}

func openDemand(path string, always []string, hotFor time.Duration) (*demand, error) {
	d := &demand{path: path, always: always, hotFor: hotFor, kick: make(chan struct{}, 1), last: map[string]time.Time{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &d.last); err != nil {
		slog.Warn("demand file unreadable, starting empty", "path", path, "err", err)
		d.last = map[string]time.Time{}
	}
	return d, nil
}

// touch records a request for regions, kicking the scraper when one of them was cold.
func (d *demand) touch(regions []string, now time.Time) {
	d.mu.Lock()
	woke := false
	for _, r := range regions {
		woke = woke || !d.hotLocked(r, now)
		// Only the first view per minute marks the file dirty.
		if now.Sub(d.last[r]) > time.Minute {
			d.last[r], d.dirty = now, true
		}
	}
	d.mu.Unlock()
	if woke {
		select {
		case d.kick <- struct{}{}:
		default:
		}
	}
}

func (d *demand) hot(region string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hotLocked(region, now)
}

func (d *demand) hotLocked(region string, now time.Time) bool {
	return slices.Contains(d.always, region) || now.Sub(d.last[region]) < d.hotFor
}

// save writes the file if anything changed since the last save.
func (d *demand) save() error {
	d.mu.Lock()
	if !d.dirty {
		d.mu.Unlock()
		return nil
	}
	b, err := json.Marshal(d.last)
	d.dirty = false
	d.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o750); err != nil {
		return err
	}
	tmp := d.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, d.path)
}

// tiers is when a cinema is due again.
type tiers struct {
	hot, cold time.Duration // since its last success
	retry     time.Duration // since its last attempt, after a failure
	slack     time.Duration // due this much early, so a tick/timer period never adds a whole interval
}

// due picks the cinemas to scrape now: every due hot cinema, then at most coldMax due cold
// ones (never-fetched first, then the oldest data).
func due(cinemas []scrape.Cinema, st *store.Store, d *demand, t tiers, coldMax int, now time.Time) []scrape.Cinema {
	type cand struct {
		c       scrape.Cinema
		updated time.Time
	}
	var hot, cold []cand
	for _, c := range cinemas {
		isHot := d.hot(c.Region, now)
		every := t.cold
		if isHot {
			every = t.hot
		}
		r, ok := st.Get(c.Name)
		if ok && (now.Sub(r.Updated) < every-t.slack || now.Sub(r.Attempted) < min(every, t.retry)-t.slack) {
			continue
		}
		if isHot {
			hot = append(hot, cand{c, r.Updated})
		} else {
			cold = append(cold, cand{c, r.Updated})
		}
	}
	slices.SortStableFunc(cold, func(a, b cand) int { return a.updated.Compare(b.updated) })
	out := make([]scrape.Cinema, 0, len(hot)+min(len(cold), coldMax))
	for _, x := range hot {
		out = append(out, x.c)
	}
	for _, x := range cold[:min(len(cold), max(coldMax, 0))] {
		out = append(out, x.c)
	}
	return out
}
