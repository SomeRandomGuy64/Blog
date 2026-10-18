package discover

import (
	"fmt"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/scrape"
)

func light(s *scrape.Session, log func(string, ...any)) ([]chainfile.Entry, error) {
	r, err := get(s, "https://www.thelight.co.uk/")
	if err != nil {
		return nil, err
	}
	places := lightPlaces(r.Body)
	if len(places) == 0 {
		return nil, fmt.Errorf("no branches on the landing page")
	}
	var mu sync.Mutex
	var out []chainfile.Entry
	each(perHost, places, func(_ int, place string) {
		sub := strings.ToLower(strings.ReplaceAll(place, " ", ""))
		e := chainfile.Entry{Name: "The Light " + place, Slug: sub, URL: "https://" + sub + ".thelight.co.uk/"}
		r, err := get(s, e.URL)
		if err != nil {
			log("%s: %v", e.URL, err)
			return
		}
		if a := lightAddress(r.Body); a != "" {
			e.Postcode, e.Address = findPostcode(a), joinAddress(a)
		} else {
			log("%s: no address", e.URL)
		}
		mu.Lock()
		out = append(out, e)
		mu.Unlock()
	})
	return out, nil
}

// lightPlaces lists the branch names on the landing page's chooser buttons.
func lightPlaces(body []byte) []string {
	doc, err := scrape.Doc(body)
	if err != nil {
		return nil
	}
	var out []string
	doc.Find(`button[name="go"] strong`).Each(func(_ int, b *goquery.Selection) {
		if t := strings.TrimSpace(b.Text()); t != "" {
			out = append(out, t)
		}
	})
	return out
}

// lightAddress is a branch page's `<p class="address">` text.
func lightAddress(body []byte) string {
	doc, err := scrape.Doc(body)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(spaces.ReplaceAllString(doc.Find("p.address").First().Text(), " "))
}
