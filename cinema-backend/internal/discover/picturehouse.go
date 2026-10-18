package discover

import (
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/scrape"
)

var (
	phLink    = regexp.MustCompile(`picturehouses\.com/cinema/([a-z0-9-]+)`)
	phID      = regexp.MustCompile(`CINEMA_ID\s*=\s*'([^']+)'`)
	phTitle   = regexp.MustCompile(`<title>([^<|]+)`)
	phMap     = regexp.MustCompile(`maps\.google\.com/maps\?q=(-?[0-9.]+),\s*(-?[0-9.]+)`)
	phAddress = regexp.MustCompile(`(?s)<div class="cinemaAdrass">(.*?)</div>`)
	phBr      = regexp.MustCompile(`(?i)<br\s*/?>`)
	tags      = regexp.MustCompile(`<[^>]*>`)
)

func picturehouse(s *scrape.Session, log func(string, ...any)) ([]chainfile.Entry, error) {
	const base = "https://www.picturehouses.com/cinema"
	r, err := get(s, base)
	if err != nil {
		return nil, err
	}
	var slugs []string
	for _, m := range phLink.FindAllStringSubmatch(r.Text(), -1) {
		if !slices.Contains(slugs, m[1]) {
			slugs = append(slugs, m[1])
		}
	}
	if len(slugs) == 0 {
		return nil, fmt.Errorf("no cinema links on %s", base)
	}
	var mu sync.Mutex
	var out []chainfile.Entry
	each(perHost, slugs, func(_ int, slug string) {
		e := chainfile.Entry{Slug: slug, URL: base + "/" + slug}
		r, err := get(s, e.URL)
		if err != nil {
			log("%s: %v", e.URL, err)
			return
		}
		parsePicturehousePage(&e, r.Body)
		if e.ID == "" {
			log("%s: no CINEMA_ID, skipped", e.URL)
			return
		}
		if r, err := get(s, e.URL+"/information"); err != nil {
			log("%s: %v", e.URL, err)
		} else {
			parsePicturehouseInfo(&e, r.Body)
		}
		mu.Lock()
		out = append(out, e)
		mu.Unlock()
	})
	return out, nil
}

// parsePicturehousePage reads the cinema id and name ("Picturehouse at FACT Liverpool |
// Picturehouse" → its first part; Picturehouse names already carry the brand).
func parsePicturehousePage(e *chainfile.Entry, body []byte) {
	if m := phID.FindSubmatch(body); m != nil {
		e.ID = string(m[1])
	}
	if m := phTitle.FindSubmatch(body); m != nil {
		e.Name = strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(string(m[1])), " "))
	}
}

// parsePicturehouseInfo reads the coordinates and address off the /information page.
func parsePicturehouseInfo(e *chainfile.Entry, body []byte) {
	if m := phMap.FindSubmatch(body); m != nil {
		e.Lat, _ = strconv.ParseFloat(string(m[1]), 64)
		e.Lng, _ = strconv.ParseFloat(string(m[2]), 64)
	}
	if m := phAddress.FindSubmatch(body); m != nil {
		var lines []string
		for _, l := range phBr.Split(string(m[1]), -1) {
			l = strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(l, "")))
			if l != "" {
				lines = append(lines, l)
			}
		}
		lines = slices.DeleteFunc(lines, func(l string) bool { return strings.EqualFold(l, e.Name) })
		e.Postcode = findPostcode(strings.Join(lines, ", "))
		e.Address = joinAddress(lines...)
	}
}
