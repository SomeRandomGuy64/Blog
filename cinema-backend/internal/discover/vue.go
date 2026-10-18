package discover

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/scrape"
)

var (
	vueSlug    = regexp.MustCompile(`/cinema/([a-z0-9-]+)/`)
	vueMap     = regexp.MustCompile(`maps\.apple\.com/\?q=(-?[0-9.]+),\s*(-?[0-9.]+)`)
	vueAddress = regexp.MustCompile(`"cinemaAddress":\{"value":("(?:[^"\\]|\\.)*")`)
	vueParen   = regexp.MustCompile(`\s*\(([^)]*)\)`)
	vueO2      = regexp.MustCompile(`\b02\b`)
)

type vueCinema struct {
	CinemaID      string `json:"cinemaId"`
	CinemaName    string `json:"cinemaName"`
	FullName      string `json:"fullName"`
	VenueCurrency string `json:"venueCurrency"`
	WhatsOnURL    string `json:"whatsOnUrl"`
}

func vue(s *scrape.Session, log func(string, ...any)) ([]chainfile.Entry, error) {
	const list = "https://www.myvue.com/api/microservice/showings/cinemas"
	r, err := s.Get(list)
	if err == nil && r.Status == 401 { // needs the anonymous token cookie a page visit hands out
		if _, err = s.Get("https://www.myvue.com/"); err == nil {
			r, err = s.Get(list)
		}
	}
	if err == nil {
		err = r.OK()
	}
	if err != nil {
		return nil, err
	}
	var j struct {
		Result []struct {
			Cinemas []vueCinema `json:"cinemas"`
		} `json:"result"`
	}
	if err := r.JSON(&j); err != nil {
		return nil, err
	}
	var cs []vueCinema
	for _, g := range j.Result {
		for _, c := range g.Cinemas {
			if c.VenueCurrency != "GBP" {
				log("dropping %s: currency %s", c.CinemaName, c.VenueCurrency)
				continue
			}
			cs = append(cs, c)
		}
	}
	if len(cs) == 0 {
		return nil, fmt.Errorf("no cinemas")
	}
	names := vueNames(cs)
	var mu sync.Mutex
	var out []chainfile.Entry
	each(perHost, cs, func(i int, c vueCinema) {
		e := chainfile.Entry{Name: names[i], ID: c.CinemaID, URL: c.WhatsOnURL}
		if m := vueSlug.FindStringSubmatch(c.WhatsOnURL); m == nil {
			log("%s: odd whatsOnUrl %q", e.Name, c.WhatsOnURL)
		} else if r, err := get(s, "https://www.myvue.com/cinema/"+m[1]+"/getting-here"); err != nil {
			log("%s: %v", e.Name, err)
		} else {
			parseVueGettingHere(&e, r.Body)
		}
		mu.Lock()
		out = append(out, e)
		mu.Unlock()
	})
	return out, nil
}

// vueNames names each cinema from its cinemaName, falling back to fullName for clashes.
func vueNames(cs []vueCinema) []string {
	names := make([]string, len(cs))
	count := map[string]int{}
	for i, c := range cs {
		names[i] = vueName(c.CinemaName)
		count[names[i]]++
	}
	for i, c := range cs {
		if count[names[i]] > 1 {
			names[i] = vueName(c.FullName)
		}
	}
	return names
}

// vueName tidies a Vue cinemaName: "London - Islington (Angel Central)" → "Vue Islington Angel
// Central"; long parentheticals ("(Great Northern Leisure Park)") are dropped.
func vueName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "Vue ")
	if rest, ok := strings.CutPrefix(s, "London -"); ok {
		s = strings.TrimSpace(rest)
	}
	s = vueParen.ReplaceAllStringFunc(s, func(p string) string {
		in := strings.TrimSpace(vueParen.FindStringSubmatch(p)[1])
		if len(strings.Fields(in)) > 3 {
			return ""
		}
		return " " + in
	})
	return chainName("Vue", vueO2.ReplaceAllString(s, "O2"))
}

// parseVueGettingHere reads the coordinates and address off a getting-here page.
func parseVueGettingHere(e *chainfile.Entry, body []byte) {
	if m := vueMap.FindSubmatch(body); m != nil {
		e.Lat, _ = strconv.ParseFloat(string(m[1]), 64)
		e.Lng, _ = strconv.ParseFloat(string(m[2]), 64)
	}
	if m := vueAddress.FindSubmatch(body); m != nil {
		var a string
		if json.Unmarshal(m[1], &a) == nil {
			var lines []string
			for _, l := range strings.FieldsFunc(a, func(r rune) bool { return r == '\r' || r == '\n' }) {
				if !strings.EqualFold(strings.TrimSpace(l), e.Name) {
					lines = append(lines, l)
				}
			}
			e.Postcode = findPostcode(a)
			e.Address = joinAddress(lines...)
		}
	}
}
