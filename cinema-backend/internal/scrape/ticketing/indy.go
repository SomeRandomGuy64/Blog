package ticketing

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

const indyQuery = `query ($date: String, $siteIds: [ID]) {
  showingsForDate(date: $date, siteIds: $siteIds) {
    data { id time published private screen { id name } showingBadges { id title displayName } movie { id name urlSlug duration } }
  }
}`

var (
	indyBundleRe = regexp.MustCompile(`src="?(/js/app\.[0-9a-f]+\.js)`)
	indyIDsRe    = regexp.MustCompile(`siteId:(\w+),circuitId:(\w+),appMode`)
	privateHire  = regexp.MustCompile(`(?i)\bprivate hire\b`)
)

// indySiteIDs: Indy Systems (Savoy / Rex / Northern Light) bake the site + circuit ids into
// each host's app bundle, as minified variables assigned in the same module.
func indySiteIDs(s *scrape.Session, baseURL string) (site, circuit string, err error) {
	page, err := s.GetOK(baseURL + "/")
	if err != nil {
		return "", "", err
	}
	src := indyBundleRe.FindStringSubmatch(page.Text())
	if src == nil {
		return "", "", fmt.Errorf("indy: no app bundle on %s", baseURL)
	}
	jsr, err := s.GetOK(baseURL + src[1])
	if err != nil {
		return "", "", err
	}
	js := jsr.Text()
	m := indyIDsRe.FindStringSubmatchIndex(js)
	if m == nil {
		return "", "", fmt.Errorf("indy: no siteId in %s", src[1])
	}
	mod := js[max(strings.LastIndex(js[:m[0]], `"use strict"`), 0):m[0]]
	var ids [2]string
	for i := range ids {
		v := js[m[2+2*i]:m[3+2*i]]
		vm := regexp.MustCompile(`[,;\s]` + regexp.QuoteMeta(v) + `=(?:\w+\?)?"(\d+)"`).FindStringSubmatch(mod)
		if vm == nil {
			return "", "", fmt.Errorf("indy: can't resolve %s in %s", v, src[1])
		}
		ids[i] = vm[1]
	}
	return ids[0], ids[1], nil
}

type indyResp struct {
	Data struct {
		ShowingsForDate *struct {
			Data []struct {
				ID        any    `json:"id"`
				Time      string `json:"time"`
				Published any    `json:"published"`
				Private   any    `json:"private"`
				Screen    *struct {
					Name string `json:"name"`
				} `json:"screen"`
				Badges []struct {
					Title       string `json:"title"`
					DisplayName string `json:"displayName"`
				} `json:"showingBadges"`
				Movie *struct {
					Name     string `json:"name"`
					URLSlug  string `json:"urlSlug"`
					Duration any    `json:"duration"`
				} `json:"movie"`
			} `json:"data"`
		} `json:"showingsForDate"`
	} `json:"data"`
	Errors any `json:"errors"`
	Error  any `json:"error"`
}

// Indy returns the fetcher for one Indy Systems host (GraphQL showingsForDate).
func Indy(baseURL string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		site, circuit, err := indySiteIDs(s, baseURL)
		if err != nil {
			return nil, err
		}
		hdr := []scrape.Opt{scrape.Header("site-id", site), scrape.Header("circuit-id", circuit),
			scrape.Header("client-type", "consumer"), scrape.Header("origin", baseURL),
			scrape.Header("referer", baseURL+"/")}
		var out []scrape.Show
		seen := map[string]bool{}
		to = scrape.DailyTo(from, to)
		// the API is UTC: pad a day each side
		for _, d := range scrape.Days(from.AddDate(0, 0, -1), to.AddDate(0, 0, 1)) {
			body := map[string]any{"query": indyQuery,
				"variables": map[string]any{"date": d.Format(scrape.DateFmt), "siteIds": []string{site}}}
			var j indyResp
			if err := s.PostJSON(baseURL+"/graphql", &j, append(hdr, scrape.JSONBody(body))...); err != nil {
				return nil, err
			}
			if truthy(j.Errors) || truthy(j.Error) {
				e := j.Errors
				if !truthy(e) {
					e = j.Error
				}
				return nil, fmt.Errorf("%s: %v", d.Format(scrape.DateFmt), e)
			}
			if j.Data.ShowingsForDate == nil {
				continue
			}
			for _, sh := range j.Data.ShowingsForDate.Data {
				id := fmt.Sprint(sh.ID)
				if seen[id] || truthy(sh.Private) || !truthy(sh.Published) {
					continue
				}
				seen[id] = true
				if sh.Movie == nil {
					continue
				}
				film := strings.TrimSpace(sh.Movie.Name)
				if film == "" || privateHire.MatchString(film) {
					continue
				}
				t, err := time.Parse(time.RFC3339Nano, sh.Time)
				if err != nil {
					return nil, fmt.Errorf("indy: bad time %q", sh.Time)
				}
				screen := ""
				if sh.Screen != nil {
					screen = sh.Screen.Name
				}
				// Rex badges every showing with its screen name ("Balcony"/"Ballroom") - not a format
				var raw []string
				for _, b := range sh.Badges {
					x := b.Title
					if x == "" {
						x = b.DisplayName
					}
					if x != "" && !(screen != "" && strings.Contains(strings.ToLower(screen), strings.ToLower(x))) {
						raw = append(raw, x)
					}
				}
				u := ""
				if sh.Movie.URLSlug != "" {
					u = baseURL + "/movie/" + sh.Movie.URLSlug
				}
				out = append(out, scrape.Show{Film: film, Start: t.In(scrape.London).Format(scrape.TimeFmt),
					Screen: screen, Formats: raw, URL: u, Duration: scrape.ToInt(sh.Movie.Duration)})
			}
		}
		return out, nil
	}
}
