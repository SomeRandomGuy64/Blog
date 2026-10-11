// Package week is the Track calendar: weeks run Sunday 03:00 UTC → next Sunday 03:00 UTC
// (Saturday evening in the Americas) and are labelled by the Saturday before their start, as
// "YYYY-MM-DD". A Key is that Saturday at 00:00 UTC.
package week

import (
	"errors"
	"time"
)

const (
	layout = "2006-01-02"
	// startOffset is from the label Saturday's midnight to the week's start, Sunday 03:00 UTC.
	startOffset = 24*time.Hour + 3*time.Hour
	Length      = 7 * 24 * time.Hour
	Days        = 7
)

// Key identifies a week: its label Saturday at 00:00 UTC.
type Key struct{ sat time.Time }

// Earliest is the first valid key, the Saturday 2000-01-01.
var Earliest = Key{time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}

// Of returns the week containing t.
func Of(t time.Time) Key {
	u := t.UTC().Add(-3 * time.Hour) // now Sunday 00:00 is the boundary
	sun := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -int(u.Weekday()))
	return Key{sun.AddDate(0, 0, -1)}
}

var (
	ErrFormat   = errors.New("week must be a date like 2026-10-10")
	ErrSaturday = errors.New("week must be a Saturday (the day before the week's Sunday 03:00 UTC start)")
	ErrRange    = errors.New("week must be between 2000-01-01 and the current week")
)

// Parse validates a key: a Saturday, no earlier than 2000-01-01 and no later than the week
// containing now.
func Parse(s string, now time.Time) (Key, error) {
	t, err := time.Parse(layout, s)
	if err != nil || t.Format(layout) != s {
		return Key{}, ErrFormat
	}
	if t.Weekday() != time.Saturday {
		return Key{}, ErrSaturday
	}
	k := Key{t}
	if k.Before(Earliest) || Of(now).Before(k) {
		return Key{}, ErrRange
	}
	return k, nil
}

func (k Key) String() string    { return k.sat.Format(layout) }
func (k Key) Start() time.Time  { return k.sat.Add(startOffset) }
func (k Key) End() time.Time    { return k.Start().Add(Length) }
func (k Key) Add(weeks int) Key { return Key{k.sat.AddDate(0, 0, 7*weeks)} }
func (k Key) Before(o Key) bool { return k.sat.Before(o.sat) }
func (k Key) IsZero() bool      { return k.sat.IsZero() }
func (k Key) Contains(t time.Time) bool {
	return !t.Before(k.Start()) && t.Before(k.End())
}

// Day returns day i's window, [start + i·24h, start + (i+1)·24h).
func (k Key) Day(i int) (from, to time.Time) {
	from = k.Start().Add(time.Duration(i) * 24 * time.Hour)
	return from, from.Add(24 * time.Hour)
}

// DayIndex is the day of the week t falls in, or -1 if it is outside the week.
func (k Key) DayIndex(t time.Time) int {
	if !k.Contains(t) {
		return -1
	}
	return int(t.Sub(k.Start()) / (24 * time.Hour))
}

// DayLabels are the label dates of the 7 days: the Saturday key, then Sunday … Friday.
// Deliberate: day i runs from Sunday 03:00 UTC + i days, so each label is the date before most
// of its window, matching the MvJ dojo's leaderboard calendar this data is kept in step with.
// Don't shift these to Sunday … Saturday.
func (k Key) DayLabels() []string {
	out := make([]string, Days)
	for i := range out {
		out[i] = k.sat.AddDate(0, 0, i).Format(layout)
	}
	return out
}

// Range lists the keys from..to inclusive (empty if to is before from).
func Range(from, to Key) []Key {
	var out []Key
	for k := from; !to.Before(k); k = k.Add(1) {
		out = append(out, k)
	}
	return out
}
