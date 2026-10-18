package discover

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/scrape"
)

var (
	vistaToken  = regexp.MustCompile(`"authToken"\s*:\s*"([^"]+)"`)
	odeonSlugRe = regexp.MustCompile(`https://www\.odeon\.co\.uk/cinemas/([a-z0-9-]+)/`)
	curzonSlug  = regexp.MustCompile(`/venues/([a-z0-9-]+)/`)
)

type vistaSite struct {
	ID   string `json:"id"`
	Name struct {
		Text string `json:"text"`
	} `json:"name"`
	Location *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"location"`
	ContactDetails struct {
		Address struct {
			Line1 string `json:"line1"`
			Line2 string `json:"line2"`
			City  string `json:"city"`
		} `json:"address"`
	} `json:"contactDetails"`
}

// vistaSites lists a Vista OCAPI's sites, with the bearer token embedded in page.
func vistaSites(s *scrape.Session, page, api string) ([]vistaSite, []byte, error) {
	r, err := s.Get(page)
	if err != nil {
		return nil, nil, err
	}
	m := vistaToken.FindSubmatch(r.Body)
	if m == nil {
		return nil, nil, fmt.Errorf("no authToken on %s (HTTP %d)", page, r.Status)
	}
	var j struct {
		Sites []vistaSite `json:"sites"`
	}
	if err := s.GetJSON(api+"/ocapi/v1/sites", &j, scrape.Header("Authorization", "Bearer "+string(m[1]))); err != nil {
		return nil, nil, err
	}
	return j.Sites, r.Body, nil
}

func (v vistaSite) entry(chain string) chainfile.Entry {
	a := v.ContactDetails.Address
	e := chainfile.Entry{Name: chainName(chain, v.Name.Text), ID: v.ID,
		Address: joinAddress(a.Line1, a.Line2, a.City), Postcode: findPostcode(a.Line1 + ", " + a.Line2 + ", " + a.City)}
	if v.Location != nil {
		e.Lat, e.Lng = v.Location.Latitude, v.Location.Longitude
	}
	return e
}

func odeon(s *scrape.Session, log func(string, ...any)) ([]chainfile.Entry, error) {
	sites, _, err := vistaSites(s, "https://www.odeon.co.uk/", "https://vwc.odeon.co.uk/WSVistaWebClient")
	if err != nil {
		return nil, err
	}
	sm, err := get(s, "https://www.odeon.co.uk/sitemap.xml")
	if err != nil {
		return nil, err
	}
	var slugs []string
	for _, m := range odeonSlugRe.FindAllStringSubmatch(sm.Text(), -1) {
		if !slices.Contains(slugs, m[1]) {
			slugs = append(slugs, m[1])
		}
	}
	var out []chainfile.Entry
	for _, v := range sites {
		e := v.entry("Odeon")
		slug, ok := matchSlug(v.Name.Text, slugs)
		if !ok {
			log("%s: no sitemap slug, guessing %q", e.Name, slug)
		}
		e.Slug, e.URL = slug, "https://www.odeon.co.uk/cinemas/"+slug+"/"
		out = append(out, e)
	}
	return out, nil
}

// matchSlug finds name's slug among slugs: an exact match, else the shortest slug containing it
// as whole hyphen-separated words ("Dudley" → "dudley-merry-hill"); ok=false falls back to
// slugify(name).
func matchSlug(name string, slugs []string) (string, bool) {
	want := slugify(name)
	if slices.Contains(slugs, want) {
		return want, true
	}
	best := ""
	for _, s := range slugs {
		if strings.Contains("-"+s+"-", "-"+want+"-") && (best == "" || len(s) < len(best)) {
			best = s
		}
	}
	if best == "" {
		return want, false
	}
	return best, true
}

func curzon(s *scrape.Session, log func(string, ...any)) ([]chainfile.Entry, error) {
	sites, page, err := vistaSites(s, "https://www.curzon.com/venues/", "https://digital-api.curzon.com")
	if err != nil {
		return nil, err
	}
	var linked []string
	for _, m := range curzonSlug.FindAllSubmatch(page, -1) {
		linked = append(linked, string(m[1]))
	}
	var out []chainfile.Entry
	for _, v := range sites {
		e := v.entry("Curzon")
		e.Slug = slugify(v.Name.Text)
		if !slices.Contains(linked, e.Slug) {
			log("%s: /venues/%s/ not linked from the venues page", e.Name, e.Slug)
		}
		e.URL = "https://www.curzon.com/venues/" + e.Slug + "/"
		out = append(out, e)
	}
	return out, nil
}
