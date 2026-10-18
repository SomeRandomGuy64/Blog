// Package feedback keeps the planner's public, login-free feedback comments. Posting is capped
// by a single quota shared by everyone (Limit per UTC day), so the list stays small and is
// kept in one JSON file.
package feedback

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	Limit    = 5    // posts per UTC day, across everyone
	MaxChars = 1000 // per post, in Unicode code points
	MaxName  = 40   // optional display name, in code points
	dayFmt   = "2006-01-02"
)

var (
	ErrEmpty   = errors.New("feedback is empty")
	ErrTooLong = errors.New("feedback is longer than 1000 characters")
	ErrName    = errors.New("name is longer than 40 characters")
	ErrQuota   = errors.New("today's feedback limit has been reached")
)

// Item is one posted comment.
type Item struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"` // "" = anonymous
	Text    string    `json:"text"`
	Created time.Time `json:"created"`
}

// Status is the public view: every comment (newest first) plus today's quota.
type Status struct {
	Items     []Item    `json:"items"`
	Limit     int       `json:"limit"`
	Used      int       `json:"used"`
	Remaining int       `json:"remaining"`
	MaxChars  int       `json:"max_chars"`
	MaxName   int       `json:"max_name"`
	ResetsAt  time.Time `json:"resets_at"` // next 00:00 UTC
	Now       time.Time `json:"now"`       // server clock, so the page can count down without trusting the viewer's
}

type state struct {
	Day   string `json:"day"`  // UTC day that Used counts
	Used  int    `json:"used"` // posts made that day (deleting one doesn't give the slot back)
	Items []Item `json:"items"`
}

// Store is the feedback file plus its in-memory copy.
type Store struct {
	path string
	Now  func() time.Time // replaceable in tests

	mu sync.Mutex
	st state
}

// Open loads dir/feedback.json (creating dir if needed).
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "feedback.json"), Now: time.Now}
	b, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &s.st); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Status returns the comments and today's quota.
func (s *Store) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status(s.Now().UTC())
}

func (s *Store) status(now time.Time) Status {
	used := 0
	if s.st.Day == now.Format(dayFmt) {
		used = s.st.Used
	}
	items := make([]Item, len(s.st.Items))
	for i, it := range s.st.Items {
		items[len(items)-1-i] = it
	}
	y, m, d := now.Date()
	return Status{Items: items, Limit: Limit, Used: used, Remaining: max(0, Limit-used), MaxChars: MaxChars, MaxName: MaxName,
		ResetsAt: time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC), Now: now.Truncate(time.Second)}
}

// Add validates and stores a comment, taking one of today's slots.
func (s *Store) Add(name, text string) (Item, Status, error) {
	name, text = clean(name, false), clean(text, true)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now().UTC()
	switch {
	case text == "":
		return Item{}, s.status(now), ErrEmpty
	case utf8.RuneCountInString(text) > MaxChars:
		return Item{}, s.status(now), ErrTooLong
	case utf8.RuneCountInString(name) > MaxName:
		return Item{}, s.status(now), ErrName
	}
	day := now.Format(dayFmt)
	next := s.st
	if next.Day != day {
		next.Day, next.Used = day, 0
	}
	if next.Used >= Limit {
		return Item{}, s.status(now), ErrQuota
	}
	var id [8]byte
	rand.Read(id[:])
	it := Item{ID: hex.EncodeToString(id[:]), Name: name, Text: text, Created: now.Truncate(time.Second)}
	next.Used++
	next.Items = append(next.Items[:len(next.Items):len(next.Items)], it)
	if err := s.write(next); err != nil {
		return Item{}, s.status(now), err
	}
	s.st = next
	return it, s.status(now), nil
}

// Delete removes a comment (moderation). It reports whether the id existed.
func (s *Store) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.st
	next.Items = nil
	for _, it := range s.st.Items {
		if it.ID != id {
			next.Items = append(next.Items, it)
		}
	}
	if len(next.Items) == len(s.st.Items) {
		return false, nil
	}
	if err := s.write(next); err != nil {
		return false, err
	}
	s.st = next
	return true, nil
}

func (s *Store) write(st state) error {
	if st.Items == nil {
		st.Items = []Item{}
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// clean drops invalid UTF-8 and control characters (keeping newlines and tabs when multiline),
// normalises line endings and trims surrounding space.
func clean(s string, multiline bool) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if multiline && (r == '\n' || r == '\t') {
			return r
		}
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
