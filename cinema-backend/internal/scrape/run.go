package scrape

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// Fetcher returns a cinema's shows for the dates from..to (inclusive, UK). Shows outside
// the window are allowed; the builder filters them.
type Fetcher func(s *Session, from, to time.Time) ([]Show, error)

// Cinema is one registry entry. Relay cinemas block datacentre IPs, so they are scraped by
// the home relay instead of the server.
type Cinema struct {
	Name  string
	Chain string
	URL   string
	Relay bool
	Fetch Fetcher
	// Location, for the travel matrix, and its region.All key.
	Lat, Lng float64
	Region   string
}

// Result is one cinema's scrape outcome (also the relay's upload format).
type Result struct {
	Name    string    `json:"name"`
	Shows   []Show    `json:"shows"`
	Error   ErrString `json:"error"`
	Fetched time.Time `json:"fetched"`
}

// ErrString is an error message that encodes "" as null.
type ErrString string

func (e ErrString) MarshalJSON() ([]byte, error) {
	if e == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(e))
}

func (e *ErrString) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*e = ""
	if s != nil {
		*e = ErrString(*s)
	}
	return nil
}

// Options tunes Run.
type Options struct {
	Workers int           // parallel cinemas (default 8)
	Timeout time.Duration // per cinema (default 5 min)
	// Progress, if set, is called as each cinema finishes.
	Progress func(r Result, inWindow int, took time.Duration)
}

// Run scrapes cinemas in parallel; one broken site never stops the rest. Results come back
// in the order of cinemas.
func Run(ctx context.Context, cinemas []Cinema, from, to time.Time, opt Options) []Result {
	if opt.Workers <= 0 {
		opt.Workers = 8
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 5 * time.Minute
	}
	memo := NewMemo()
	out := make([]Result, len(cinemas))
	sem := make(chan struct{}, opt.Workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, c := range cinemas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t0 := time.Now()
			r := FetchOne(ctx, c, from, to, opt.Timeout, memo)
			out[i] = r
			if opt.Progress != nil {
				n := 0
				for _, s := range r.Shows {
					if InWindow(s.Start, from, to) {
						n++
					}
				}
				mu.Lock()
				opt.Progress(r, n, time.Since(t0))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// FetchOne runs one cinema's fetcher with its own session and timeout, turning errors and
// panics into Result.Error.
func FetchOne(ctx context.Context, c Cinema, from, to time.Time, timeout time.Duration, memo *Memo) (r Result) {
	r = Result{Name: c.Name, Shows: []Show{}}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		r.Fetched = time.Now().UTC().Truncate(time.Second)
		if p := recover(); p != nil {
			r.Shows, r.Error = []Show{}, ErrString(errLine(fmt.Errorf("panic: %v\n%s", p, debug.Stack())))
		}
	}()
	s, err := NewSession(ctx, memo)
	if err != nil {
		r.Error = ErrString(errLine(err))
		return r
	}
	defer s.Close()
	shows, err := c.Fetch(s, from, to)
	if err != nil {
		r.Error = ErrString(errLine(err))
		return r
	}
	for i := range shows {
		shows[i].Normalize()
	}
	if shows != nil {
		r.Shows = shows
	}
	return r
}

func errLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200])
	}
	return s
}
