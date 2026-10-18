package discover

import (
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
	"sync"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/scrape"
)

var (
	reelLink   = regexp.MustCompile(`reel-cinemas\.co\.uk/([a-z0-9-]+)/`)
	reelFooter = regexp.MustCompile(`(?:©|&copy;|&#169;)\s*\d{4}\s+REEL Cinemas?\s+([^,<]+),([^<]*)`)
)

func reel(s *scrape.Session, log func(string, ...any)) ([]chainfile.Entry, error) {
	r, err := get(s, "https://reel-cinemas.co.uk/")
	if err != nil {
		return nil, err
	}
	towns := reelTowns(r.Body)
	if len(towns) == 0 {
		return nil, fmt.Errorf("no town links on the landing page")
	}
	var mu sync.Mutex
	var out []chainfile.Entry
	each(perHost, towns, func(_ int, town string) {
		base := "https://reel-cinemas.co.uk/" + town + "/"
		if _, err := get(s, base+"whatson/"); err != nil {
			if he, ok := err.(*scrape.HTTPError); !ok || he.Status != 404 { // 404: not a cinema (cookie-policy, ...)
				log("%swhatson/: %v", base, err)
			}
			return
		}
		r, err := get(s, base)
		if err != nil {
			log("%s: %v", base, err)
			return
		}
		e := chainfile.Entry{Slug: town, URL: base + "whatson/"}
		if !parseReelFooter(&e, r.Body) {
			log("%s: no footer address", base)
		}
		mu.Lock()
		out = append(out, e)
		mu.Unlock()
	})
	return out, nil
}

// reelTowns lists the landing page's first-level paths, minus WordPress plumbing.
func reelTowns(body []byte) []string {
	var out []string
	for _, m := range reelLink.FindAllStringSubmatch(string(body), -1) {
		t := m[1]
		if strings.HasPrefix(t, "wp-") || t == "feed" || slices.Contains(out, t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// parseReelFooter reads "© 2026 REEL Cinema Burnley, Unit 1, Pioneer Place, ..., BB11 1FB".
func parseReelFooter(e *chainfile.Entry, body []byte) bool {
	m := reelFooter.FindSubmatch(body)
	if m == nil {
		return false
	}
	a := html.UnescapeString(string(m[2]))
	e.Postcode, e.Address = findPostcode(a), joinAddress(a)
	e.Name = chainName("Reel", reelPlace(html.UnescapeString(string(m[1])), strings.Split(e.Address, ", ")))
	return true
}

// reelPlace drops a county the footer appends to the town ("Fareham Hampshire" → "Fareham"):
// the longest address part that starts place word-wise, else place itself.
func reelPlace(place string, parts []string) string {
	place = strings.TrimSpace(place)
	best := place
	found := false
	for _, p := range parts {
		if (p == place || strings.HasPrefix(place, p+" ")) && (!found || len(p) > len(best)) {
			best, found = p, true
		}
	}
	return best
}
