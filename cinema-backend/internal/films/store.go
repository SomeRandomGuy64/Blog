package films

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// RecheckAfter is how long a title the film database didn't know waits before being tried again.
// A title it did know is never looked up twice.
const RecheckAfter = 30 * 24 * time.Hour

var (
	ErrKey   = errors.New("bad film key")
	ErrCycle = errors.New("that would make a film the same as itself")
)

var keyRe = regexp.MustCompile(`^[a-z0-9]{1,200}$`)

// Lookup is what the film database said about a title. ID 0 means it had no exact match.
type Lookup struct {
	ID      int       `json:"id,omitempty"`
	Title   string    `json:"title,omitempty"`
	Year    string    `json:"year,omitempty"`
	Key     string    `json:"key,omitempty"` // Key(Title)
	Checked time.Time `json:"checked"`
}

// Snapshot is the saved decisions. It grows forever, so a name is only ever resolved once.
// Merge[a] = b means a is the same film as b; Merge[a] = a keeps a apart from anything the
// rules would group it with. Only the site owner changes Merge, Apart and Confirmed.
type Snapshot struct {
	Merge     map[string]string `json:"merge"`
	Apart     map[string]bool   `json:"apart"`     // PairKey(a, b): not the same, stop suggesting it
	Confirmed map[string]bool   `json:"confirmed"` // film keys whose grouping has been checked
	Lookups   map[string]Lookup `json:"lookups"`   // by Key of the base title
}

// PairKey is the order-independent key of two films.
func PairKey(a, b string) string {
	if b < a {
		a, b = b, a
	}
	return a + "|" + b
}

// Store keeps the Snapshot in one JSON file.
type Store struct {
	path string

	mu  sync.Mutex
	s   Snapshot
	gen uint64
}

// Open loads <dir>/matches.json, creating dir if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	st := &Store{path: filepath.Join(dir, "matches.json")}
	b, err := os.ReadFile(st.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(b, &st.s); err != nil {
			return nil, err
		}
	}
	if st.s.Merge == nil {
		st.s.Merge = map[string]string{}
	}
	if st.s.Apart == nil {
		st.s.Apart = map[string]bool{}
	}
	if st.s.Confirmed == nil {
		st.s.Confirmed = map[string]bool{}
	}
	if st.s.Lookups == nil {
		st.s.Lookups = map[string]Lookup{}
	}
	return st, nil
}

// Gen changes whenever the saved decisions do.
func (st *Store) Gen() uint64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.gen
}

// Snapshot returns a copy that later changes don't touch.
func (st *Store) Snapshot() *Snapshot {
	st.mu.Lock()
	defer st.mu.Unlock()
	return &Snapshot{Merge: maps.Clone(st.s.Merge), Apart: maps.Clone(st.s.Apart),
		Confirmed: maps.Clone(st.s.Confirmed), Lookups: maps.Clone(st.s.Lookups)}
}

func (st *Store) saveLocked() error {
	b, err := json.Marshal(st.s)
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, st.path); err != nil {
		return err
	}
	st.gen++
	return nil
}

func validKeys(keys ...string) error {
	for _, k := range keys {
		if !keyRe.MatchString(k) {
			return ErrKey
		}
	}
	return nil
}

// Merge records that from is the same film as to.
func (st *Store) Merge(from, to string) error {
	if err := validKeys(from, to); err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if from == to {
		return ErrCycle
	}
	for k, n := to, 0; n <= len(st.s.Merge); n++ {
		next, ok := st.s.Merge[k]
		if !ok || next == k {
			break
		}
		if next == from {
			return ErrCycle
		}
		k = next
	}
	st.s.Merge[from] = to
	delete(st.s.Apart, PairKey(from, to))
	return st.saveLocked()
}

// Split keeps k apart from whatever the rules would group it with.
func (st *Store) Split(k string) error {
	if err := validKeys(k); err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Merge[k] = k
	return st.saveLocked()
}

// Clear forgets the merge or split recorded for k.
func (st *Store) Clear(k string) error {
	if err := validKeys(k); err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.s.Merge, k)
	delete(st.s.Confirmed, k)
	return st.saveLocked()
}

// Apart records that a and b are different films.
func (st *Store) Apart(a, b string) error {
	if err := validKeys(a, b); err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Apart[PairKey(a, b)] = true
	return st.saveLocked()
}

// Confirm records that the film at key k is grouped correctly.
func (st *Store) Confirm(k string) error {
	if err := validKeys(k); err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Confirmed[k] = true
	return st.saveLocked()
}

// PutLookup saves a film database answer for a title key.
func (st *Store) PutLookup(key string, l Lookup) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Lookups[key] = l
	return st.saveLocked()
}

// Missing lists the title keys still to ask the film database about, oldest answer last.
func (st *Store) Missing(titles map[string]string, now time.Time) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []string
	for k := range titles {
		l, ok := st.s.Lookups[k]
		if !ok || (l.ID == 0 && now.Sub(l.Checked) > RecheckAfter) {
			out = append(out, k)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		if c := st.s.Lookups[a].Checked.Compare(st.s.Lookups[b].Checked); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return out
}
