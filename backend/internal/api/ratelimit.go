package api

import (
	"sync"
	"time"
)

// limiter is an in-memory sliding-window counter: at most n events per key per window.
// State is lost on restart, which is fine for abuse damping on a single small server.
type limiter struct {
	n      int
	window time.Duration

	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter(n int, window time.Duration) *limiter {
	return &limiter{n: n, window: window, hits: map[string][]time.Time{}}
}

// allow records an event for key and reports whether it is within the limit. Refused events
// are not recorded, so a client hammering away regains access once its window passes.
func (l *limiter) allow(key string, now time.Time) bool {
	ok, _ := l.allowWait(key, now)
	return ok
}

// allowWait is allow that also says, when it refuses, how long until the oldest event in the
// window expires and the next one would be allowed (for Retry-After).
func (l *limiter) allowWait(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	h := live(l.hits[key], now.Add(-l.window))
	if len(h) >= l.n {
		l.hits[key] = h
		return false, h[0].Add(l.window).Sub(now)
	}
	l.hits[key] = append(h, now)
	return true, 0
}

func (l *limiter) sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, h := range l.hits {
		if h = live(h, now.Add(-l.window)); len(h) == 0 {
			delete(l.hits, k)
		} else {
			l.hits[k] = h
		}
	}
}

// live drops the (oldest-first) timestamps at or before cutoff.
func live(h []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(h) && !h[i].After(cutoff) {
		i++
	}
	return h[i:]
}
