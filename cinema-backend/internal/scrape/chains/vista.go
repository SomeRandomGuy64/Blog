// Package chains holds the fetchers for the big chains' platforms.
package chains

import (
	"fmt"
	"net/url"
	"regexp"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

var (
	vistaToken = regexp.MustCompile(`"authToken"\s*:\s*"([^"]+)"`)
	audioSuffx = regexp.MustCompile(`(?i)\s*\(audio\)`)
)

// Odeon is Vista OCAPI at Odeon.
func Odeon(slug, siteID string) scrape.Fetcher {
	return Vista("https://www.odeon.co.uk/cinemas/"+slug+"/", "https://vwc.odeon.co.uk/WSVistaWebClient", siteID)
}

type vistaText struct {
	Text string `json:"text"`
}

type vistaDay struct {
	Showtimes []struct {
		FilmID            string   `json:"filmId"`
		ScreenID          string   `json:"screenId"`
		AttributeIDs      []string `json:"attributeIds"`
		Requires3dGlasses bool     `json:"requires3dGlasses"`
		Schedule          struct {
			StartsAt   string `json:"startsAt"` // ads start
			FilmEndsAt string `json:"filmEndsAt"`
			EndsAt     string `json:"endsAt"`
		} `json:"schedule"`
	} `json:"showtimes"`
	RelatedData struct {
		Films []struct {
			ID               string    `json:"id"`
			Title            vistaText `json:"title"`
			RuntimeInMinutes any       `json:"runtimeInMinutes"`
		} `json:"films"`
		Attributes []struct {
			ID   string    `json:"id"`
			Name vistaText `json:"name"`
		} `json:"attributes"`
		Screens []struct {
			ID   string    `json:"id"`
			Name vistaText `json:"name"`
		} `json:"screens"`
	} `json:"relatedData"`
}

// Vista fetches Odeon/Curzon showtimes from Vista OCAPI, using the bearer token embedded in
// any page of the site.
func Vista(page, api, siteID string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		r, err := s.Get(page) // any page embeds the token, even a 404
		if err != nil {
			return nil, err
		}
		m := vistaToken.FindSubmatch(r.Body)
		if m == nil {
			return nil, fmt.Errorf("vista: no authToken on %s (HTTP %d)", page, r.Status)
		}
		auth := scrape.Header("Authorization", "Bearer "+string(m[1]))
		var out []scrape.Show
		for _, d := range scrape.Days(from, to) {
			var j vistaDay
			u := api + "/ocapi/v1/showtimes/by-business-date/" + d.Format(scrape.DateFmt)
			if err := s.GetJSON(u, &j, scrape.Query(url.Values{"siteIds": {siteID}}), auth); err != nil {
				return nil, err
			}
			rel := j.RelatedData
			films, runtime := map[string]string{}, map[string]int{}
			for _, f := range rel.Films {
				films[f.ID], runtime[f.ID] = f.Title.Text, scrape.ToInt(f.RuntimeInMinutes)
			}
			attrs, screens := map[string]string{}, map[string]string{}
			for _, a := range rel.Attributes {
				attrs[a.ID] = a.Name.Text
			}
			for _, x := range rel.Screens {
				screens[x.ID] = x.Name.Text
			}
			for _, st := range j.Showtimes {
				var raw []string
				for _, a := range st.AttributeIDs {
					raw = append(raw, audioSuffx.ReplaceAllString(attrs[a], ""))
				}
				if st.Requires3dGlasses {
					raw = append(raw, "3D")
				}
				film, ok := films[st.FilmID]
				if !ok {
					film = st.FilmID
				}
				end := st.Schedule.FilmEndsAt // credits end, local offset
				if end == "" {
					end = st.Schedule.EndsAt
				}
				out = append(out, scrape.Show{Film: film, Start: st.Schedule.StartsAt, Screen: screens[st.ScreenID],
					Formats: scrape.Tags(raw, false), URL: page, End: end, Duration: runtime[st.FilmID]})
			}
		}
		return out, nil
	}
}
