package chains

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

var phToken = regexp.MustCompile(`var token = "([^"]+)"`)

var phAlias = map[string]string{"Audio D": "", "Watch Baby": "Parent & Baby", "Toddler Ti": "Toddler Time",
	"Silver Scr": "Silver Screen", "Kids' Club": "Kids' Club", "LiveSat": "Event Cinema",
	"DogFriend": "Dog Friendly", "reDiscover": "Classic", "Picture Do": "Picture Doc"}

// flexStr accepts a JSON string or number ("013" vs 13 ids).
type flexStr string

func (f *flexStr) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexStr(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexStr(n.String())
	return nil
}

// Picturehouse fetches one cinema from the site's ajax API (needs the page's XSRF token).
func Picturehouse(cinemaID, slug string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		page, err := s.GetOK("https://www.picturehouses.com/")
		if err != nil {
			return nil, err
		}
		m := phToken.FindSubmatch(page.Body)
		if m == nil {
			return nil, fmt.Errorf("picturehouse: no XSRF token")
		}
		token := string(m[1])
		h := phHeaders(token)
		var j struct {
			Movies []struct {
				Title           string  `json:"Title"`
				ScheduledFilmID flexStr `json:"ScheduledFilmId"`
				ShowTimes       []struct {
					CinemaID               flexStr  `json:"CinemaId"`
					Showtime               string   `json:"Showtime"`
					ScreenName             string   `json:"ScreenName"`
					SessionAttributesNames []string `json:"SessionAttributesNames"`
				} `json:"show_times"`
			} `json:"movies"`
		}
		if err := s.PostJSON("https://www.picturehouses.com/api/scheduled-movies-ajax", &j,
			scrape.Form(url.Values{"cinema_id": {cinemaID}}), h[0], h[1],
			scrape.Timeout(120*time.Second)); err != nil {
			return nil, err
		}
		rt := phRuntimes(s, token)
		var out []scrape.Show
		for _, mv := range j.Movies {
			for _, st := range mv.ShowTimes {
				if string(st.CinemaID) != cinemaID {
					continue
				}
				raw := make([]string, 0, len(st.SessionAttributesNames))
				for _, a := range st.SessionAttributesNames {
					if v, ok := phAlias[a]; ok {
						a = v
					}
					raw = append(raw, a)
				}
				out = append(out, scrape.Show{Film: mv.Title, Start: st.Showtime, Screen: st.ScreenName,
					Formats: scrape.Tags(raw, false), URL: "https://www.picturehouses.com/cinema/" + slug,
					Duration: rt[string(mv.ScheduledFilmID)]})
			}
		}
		return out, nil
	}
}

func phHeaders(token string) [2]scrape.Opt {
	return [2]scrape.Opt{scrape.Header("X-XSRF-TOKEN", token), scrape.Header("X-Requested-With", "XMLHttpRequest")}
}

// phRuntimes maps film id -> runtime. get-movies-ajax returns every film at every
// Picturehouse (~5 MB), so it is fetched once per run; runtimes are optional, so a failure
// just leaves them empty (and isn't retried per cinema).
func phRuntimes(s *scrape.Session, token string) map[string]int {
	v, _ := s.Memo("picturehouse-runtimes", func() (any, error) {
		rt := map[string]int{}
		var j struct {
			Movies []struct {
				ScheduledFilmID flexStr `json:"ScheduledFilmId"`
				RunTime         any     `json:"RunTime"`
			} `json:"movies"`
		}
		h := phHeaders(token)
		if err := s.PostJSON("https://www.picturehouses.com/api/get-movies-ajax", &j, h[0], h[1],
			scrape.Form(url.Values{"cinema_id": {"013"}, "start_date": {"show_all_dates"}}),
			scrape.Timeout(120*time.Second)); err == nil {
			for _, m := range j.Movies {
				rt[string(m.ScheduledFilmID)] = scrape.ToInt(m.RunTime)
			}
		}
		return rt, nil
	})
	return v.(map[string]int)
}
