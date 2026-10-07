package chains

import (
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

type vueFilms struct {
	Result []struct {
		FilmTitle     string `json:"filmTitle"`
		RunningTime   any    `json:"runningTime"`
		ShowingGroups []struct {
			Sessions []struct {
				StartTime  string `json:"startTime"`
				EndTime    string `json:"endTime"` // incl. ads
				ScreenName string `json:"screenName"`
				BookingURL string `json:"bookingUrl"`
				Duration   any    `json:"duration"`
				Attributes []struct {
					Name          string `json:"name"`
					AttributeType string `json:"attributeType"`
				} `json:"attributes"`
			} `json:"sessions"`
		} `json:"showingGroups"`
	} `json:"result"`
}

// Vue fetches one Vue cinema from the myvue microservice API, a day at a time.
func Vue(cinemaID string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		var out []scrape.Show
		for _, d := range scrape.Days(from, to) {
			u := "https://www.myvue.com/api/microservice/showings/cinemas/" + cinemaID + "/films?showingDate=" +
				d.Format(scrape.DateFmt) + "T00:00:00&minEmbargoLevel=3&includesSession=true&includeSessionAttributes=true"
			r, err := s.Get(u)
			if err == nil && r.Status == 401 { // needs the anonymous token cookie a page visit hands out
				if _, err = s.Get("https://www.myvue.com/"); err == nil {
					r, err = s.Get(u)
				}
			}
			if err == nil {
				err = r.OK()
			}
			if err != nil {
				return nil, err
			}
			var j vueFilms
			if err := r.JSON(&j); err != nil {
				return nil, err
			}
			for _, f := range j.Result {
				for _, g := range f.ShowingGroups {
					for _, se := range g.Sessions {
						var raw []string
						for _, a := range se.Attributes {
							if a.AttributeType != "Movie" || strings.Contains(strings.ToLower(a.Name), "event") {
								raw = append(raw, a.Name)
							}
						}
						dur := scrape.ToInt(se.Duration)
						if dur == 0 {
							dur = scrape.ToInt(f.RunningTime)
						}
						out = append(out, scrape.Show{Film: f.FilmTitle, Start: se.StartTime, Screen: se.ScreenName,
							Formats: scrape.Tags(raw, false), URL: "https://www.myvue.com" + se.BookingURL,
							End: se.EndTime, Duration: dur})
					}
				}
			}
		}
		return out, nil
	}
}
