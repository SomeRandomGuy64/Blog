// Package chainfile is the format of internal/registry/chains.json: every big-chain cinema in
// the UK, generated from the chains' own APIs by `cinema discover` and embedded in the binaries.
package chainfile

import (
	"bytes"
	"encoding/json"
	"os"
)

// Entry is one discovered cinema. Chain + ID identify it across re-discoveries (its Name is
// kept once published, because the store and visitors' saved settings key on it).
//
// What the fetcher needs per chain:
//
//	Odeon        ID = Vista site id ("856"), Slug = odeon.co.uk/cinemas/<slug>/
//	Curzon       ID = Vista site id ("KNU1"), Slug = curzon.com/venues/<slug>/
//	Cineworld, Everyman, Showcase   ID = Webedia theater id ("X06X2"); URL = the cinema page
//	Vue          ID = cinemaId ("10091"); URL = its whats-on page
//	Picturehouse ID = cinema id ("013"), Slug = picturehouses.com/cinema/<slug>
//	The Light    Slug = subdomain ("newbrighton")
//	Reel         Slug = town path ("rochdale")
type Entry struct {
	Name     string  `json:"name"`  // display name, chain first: "Odeon Oldham"
	Chain    string  `json:"chain"` // Odeon, Curzon, Cineworld, Everyman, Showcase, Vue, Picturehouse, The Light, Reel
	ID       string  `json:"id,omitempty"`
	Slug     string  `json:"slug,omitempty"`
	URL      string  `json:"url"` // the cinema's own page (also used as booking fallback)
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Postcode string  `json:"postcode,omitempty"`
	Address  string  `json:"address,omitempty"`
	Region   string  `json:"region"` // region.All key
}

// File is the whole document.
type File struct {
	Generated string  `json:"generated"` // YYYY-MM-DD of the discovery run
	Cinemas   []Entry `json:"cinemas"`   // sorted by Chain, then Name
}

// Parse decodes a chains.json document.
func Parse(b []byte) (File, error) {
	var f File
	err := json.Unmarshal(b, &f)
	return f, err
}

// Load reads a chains.json file.
func Load(path string) (File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	return Parse(b)
}

// Save writes f as indented JSON, one cinema per line-group, for reviewable diffs.
func Save(path string, f File) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(f); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
