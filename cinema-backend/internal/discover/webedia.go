package discover

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/scrape"
)

// movieTheater is a page's schema.org MovieTheater JSON-LD.
type movieTheater struct {
	Name    string `json:"name"`
	Address struct {
		StreetAddress   string `json:"streetAddress"`
		AddressLocality string `json:"addressLocality"`
		PostalCode      string `json:"postalCode"`
		AddressCountry  string `json:"addressCountry"`
	} `json:"address"`
	Geo struct {
		Latitude  json.Number `json:"latitude"`
		Longitude json.Number `json:"longitude"`
	} `json:"geo"`
}

// findMovieTheater returns the first MovieTheater in the page's ld+json scripts.
func findMovieTheater(body []byte) (movieTheater, bool) {
	doc, err := scrape.Doc(body)
	if err != nil {
		return movieTheater{}, false
	}
	var mt movieTheater
	found := false
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, sc *goquery.Selection) bool {
		var raw any
		if json.Unmarshal([]byte(sc.Text()), &raw) != nil {
			return true
		}
		var walk func(any) bool
		walk = func(v any) bool {
			switch v := v.(type) {
			case []any:
				for _, x := range v {
					if walk(x) {
						return true
					}
				}
			case map[string]any:
				if isType(v["@type"], "MovieTheater") {
					b, _ := json.Marshal(v)
					found = json.Unmarshal(b, &mt) == nil
					return found
				}
				return walk(v["@graph"])
			}
			return false
		}
		return !walk(raw)
	})
	mt.Name = html.UnescapeString(mt.Name)
	return mt, found
}

func isType(t any, want string) bool {
	switch t := t.(type) {
	case string:
		return t == want
	case []any:
		for _, x := range t {
			if x == want {
				return true
			}
		}
	}
	return false
}

// webedia lists a Webedia (Gatsby) site's cinemas from its listing page-data, then reads each
// cinema page's JSON-LD for the name and location.
func webedia(chain, base, list string) finder {
	return func(s *scrape.Session, log func(string, ...any)) ([]chainfile.Entry, error) {
		var j struct {
			Result struct {
				Data struct {
					Page struct {
						ChildPages []struct {
							Slug          string `json:"slug"`
							RelatedEntity *struct {
								Typename string `json:"__typename"`
								ID       string `json:"id"`
							} `json:"relatedEntity"`
						} `json:"childPages"`
					} `json:"page"`
				} `json:"data"`
			} `json:"result"`
		}
		r, err := get(s, base+list)
		if err != nil {
			return nil, err
		}
		if err := r.JSON(&j); err != nil {
			return nil, err
		}
		var es []chainfile.Entry
		for _, p := range j.Result.Data.Page.ChildPages {
			if p.RelatedEntity == nil || p.RelatedEntity.Typename != "Theater" || p.RelatedEntity.ID == "" {
				log("%s: no theater id (not open yet?), skipped", p.Slug)
				continue
			}
			es = append(es, chainfile.Entry{ID: p.RelatedEntity.ID, URL: base + "/" + strings.Trim(p.Slug, "/") + "/"})
		}
		if len(es) == 0 {
			return nil, fmt.Errorf("no cinemas in %s", list)
		}
		var mu sync.Mutex
		var out []chainfile.Entry
		each(perHost, es, func(_ int, e chainfile.Entry) {
			r, err := get(s, e.URL)
			if err != nil {
				log("%s: %v", e.URL, err)
				mu.Lock()
				out = append(out, e) // a previous entry may fill it in
				mu.Unlock()
				return
			}
			mt, ok := findMovieTheater(r.Body)
			if !ok {
				log("%s: no MovieTheater JSON-LD", e.URL)
			} else if c := mt.Address.AddressCountry; c != "" && c != "GB" {
				log("dropping %s: country %s", e.URL, c)
				return
			} else {
				fillTheater(&e, chain, mt)
			}
			mu.Lock()
			out = append(out, e)
			mu.Unlock()
		})
		return out, nil
	}
}

func fillTheater(e *chainfile.Entry, chain string, mt movieTheater) {
	e.Name = chainName(chain, mt.Name)
	e.Lat, _ = mt.Geo.Latitude.Float64()
	e.Lng, _ = mt.Geo.Longitude.Float64()
	a := mt.Address
	e.Postcode = findPostcode(a.PostalCode)
	e.Address = joinAddress(a.StreetAddress, a.AddressLocality)
}
