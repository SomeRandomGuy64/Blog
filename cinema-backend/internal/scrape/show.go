package scrape

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Show is one raw performance as a fetcher reports it. Times are naive UK wall-clock
// "2006-01-02T15:04". End is when the film finishes (incl. ads); Duration is the film
// runtime in minutes (0 = unknown).
type Show struct {
	Film     string   `json:"film"`
	Start    string   `json:"start"`
	Screen   string   `json:"screen"`
	Formats  []string `json:"formats"`
	URL      string   `json:"url"`
	End      string   `json:"end"`
	Duration int      `json:"duration"`
}

// MarshalJSON writes empty optional fields as null, like the Python dicts.
func (s Show) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Film     string   `json:"film"`
		Start    string   `json:"start"`
		Screen   *string  `json:"screen"`
		Formats  []string `json:"formats"`
		URL      *string  `json:"url"`
		End      *string  `json:"end"`
		Duration *int     `json:"duration"`
	}{s.Film, s.Start, nullStr(s.Screen), nonNil(s.Formats), nullStr(s.URL), nullStr(s.End), nullInt(s.Duration)})
}

// UnmarshalJSON accepts null for any optional field and any JSON number for duration.
func (s *Show) UnmarshalJSON(b []byte) error {
	var v struct {
		Film     string   `json:"film"`
		Start    string   `json:"start"`
		Screen   *string  `json:"screen"`
		Formats  []string `json:"formats"`
		URL      *string  `json:"url"`
		End      *string  `json:"end"`
		Duration any      `json:"duration"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*s = Show{Film: v.Film, Start: v.Start, Formats: v.Formats, Duration: ToInt(v.Duration)}
	if v.Screen != nil {
		s.Screen = *v.Screen
	}
	if v.URL != nil {
		s.URL = *v.URL
	}
	if v.End != nil {
		s.End = *v.End
	}
	return nil
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func nonNil(x []string) []string {
	if x == nil {
		return []string{}
	}
	return x
}

// Normalize applies the Python show() rules: trimmed film, minute-precision times,
// no empty formats.
func (s *Show) Normalize() {
	s.Film = strings.TrimSpace(s.Film)
	s.Start = trunc16(s.Start)
	s.End = trunc16(s.End)
	f := s.Formats[:0:0]
	for _, x := range s.Formats {
		if x != "" {
			f = append(f, x)
		}
	}
	s.Formats = f
}

func trunc16(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

// ToInt mirrors Python's int(x) or None: numbers truncate, numeric strings parse, anything
// else (or 0) is 0.
func ToInt(x any) int {
	switch v := x.(type) {
	case nil:
		return 0
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0
		}
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
		f, _ := v.Float64()
		return int(f)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	case bool:
		if v {
			return 1
		}
	}
	return 0
}

var minsRe = regexp.MustCompile(`(?i)(\d+)\s*h(?:ours?|rs?)?\b\s*(?:(\d+)\s*m)?|(\d+)\s*m(?:in|ins|inutes)?\b`)

// Mins parses "1h 57m" / "130 mins" / "2 hours" into minutes (0 if absent).
func Mins(text string) int {
	m := minsRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	if m[1] != "" {
		h, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		return h*60 + mm
	}
	n, _ := strconv.Atoi(m[3])
	return n
}

// Dates are calendar days held as midnight UTC.
const DateFmt, TimeFmt = "2006-01-02", "2006-01-02T15:04"

// Day returns the calendar date of t (in t's location) as midnight UTC.
func Day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ParseDate parses "2006-01-02".
func ParseDate(s string) (time.Time, error) { return time.Parse(DateFmt, s) }

// Days lists every date from..to inclusive.
func Days(from, to time.Time) []time.Time {
	var out []time.Time
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}

// InWindow reports whether a show start "2006-01-02T15:04" falls on from..to.
func InWindow(start string, from, to time.Time) bool {
	if len(start) < 10 {
		return false
	}
	d := start[:10]
	return d >= from.Format(DateFmt) && d <= to.Format(DateFmt)
}

// London is the zone every listing is expressed in.
var London = mustZone("Europe/London")

func mustZone(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}
