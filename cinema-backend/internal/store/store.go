// Package store keeps each cinema's last good scrape on disk, so a failed scrape (or a relay
// that hasn't uploaded for a while) keeps serving the previous data.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fazon.nexus/cinema/internal/listings"
	"fazon.nexus/cinema/internal/scrape"
)

// Record is one cinema's persisted state.
type Record struct {
	Name      string        `json:"name"`
	Shows     []scrape.Show `json:"shows"`     // from the last successful scrape
	Updated   time.Time     `json:"updated"`   // when Shows were fetched (zero = never)
	Error     string        `json:"error"`     // latest attempt's error ("" = it succeeded)
	Attempted time.Time     `json:"attempted"` // latest attempt
	Source    string        `json:"source"`    // "server" or "relay"
}

// Store is a directory of <slug>.json files with an in-memory copy.
type Store struct {
	dir string
	mu  sync.RWMutex
	rec map[string]Record
	gen uint64 // bumps on every change (ETag input)
}

// Open loads every record in dir (creating it if needed).
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, rec: map[string]Record{}}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var r Record
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		s.rec[r.Name] = r
	}
	return s, nil
}

// Apply records a scrape result: a success replaces the shows, a failure only records the
// error and keeps the last good shows. A result older than the stored attempt is ignored.
func (s *Store) Apply(r scrape.Result, source string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.rec[r.Name]
	if !r.Fetched.IsZero() && r.Fetched.Before(rec.Attempted) {
		return rec, nil
	}
	rec.Name, rec.Attempted, rec.Error, rec.Source = r.Name, r.Fetched, string(r.Error), source
	if rec.Attempted.IsZero() {
		rec.Attempted = time.Now().UTC()
	}
	if r.Error == "" {
		rec.Shows, rec.Updated = r.Shows, rec.Attempted
	}
	if rec.Shows == nil {
		rec.Shows = []scrape.Show{}
	}
	if err := s.write(rec); err != nil {
		return rec, err
	}
	s.rec[r.Name] = rec
	s.gen++
	return rec, nil
}

func (s *Store) write(r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, slug(r.Name)+".json")
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
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
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// Entries returns the records as listings entries, plus a generation number that changes
// whenever the data does.
func (s *Store) Entries() (map[string]listings.Entry, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := make(map[string]listings.Entry, len(s.rec))
	for n, r := range s.rec {
		m[n] = listings.Entry{Shows: r.Shows, Error: r.Error, Updated: r.Updated}
	}
	return m, s.gen
}

// Get returns one record.
func (s *Store) Get(name string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rec[name]
	return r, ok
}

// Prune deletes records for cinemas no longer in keep (e.g. a closed cinema).
func (s *Store) Prune(keep func(name string) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for n := range s.rec {
		if keep(n) {
			continue
		}
		err := os.Remove(filepath.Join(s.dir, slug(n)+".json"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		delete(s.rec, n)
		s.gen++
	}
	return nil
}

// slug makes a safe file name from a cinema name ("Odeon Liverpool ONE" -> "odeon-liverpool-one").
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
