package chains

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

type webediaShow struct {
	StartsAt string   `json:"startsAt"`
	Tags     []string `json:"tags"`
	Screen   *struct {
		Name string `json:"name"`
	} `json:"screen"`
	Data struct {
		Ticketing []struct {
			Provider string   `json:"provider"`
			URLs     []string `json:"urls"`
		} `json:"ticketing"`
	} `json:"data"`
}

type webediaMovie struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Locale *struct {
		Title string `json:"title"`
	} `json:"locale"`
	Runtime float64 `json:"runtime"` // seconds
}

// Webedia fetches Cineworld / Everyman / Showcase, which all run on Webedia's boxoffice API.
func Webedia(base, theater string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		api := base + "/api/gatsby-source-boxofficeapi"
		// The theaters param must be compact JSON: the API rejects it with spaces.
		th, _ := json.Marshal(map[string]string{"id": theater, "timeZone": "Europe/London"})
		q := url.Values{"from": {from.Format(scrape.DateFmt) + "T03:00:00"},
			"to": {to.AddDate(0, 0, 1).Format(scrape.DateFmt) + "T03:00:00"}, "theaters": {string(th)}}
		var resp map[string]struct {
			Schedule map[string]map[string][]webediaShow `json:"schedule"`
		}
		if err := s.GetJSON(api+"/schedule", &resp, scrape.Query(q)); err != nil {
			return nil, err
		}
		t, ok := resp[theater]
		if !ok {
			return nil, fmt.Errorf("webedia: theater %s missing from schedule", theater)
		}
		ids := make([]string, 0, len(t.Schedule))
		for id := range t.Schedule {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		titles, runtime := map[string]string{}, map[string]int{}
		for chunk := range slices.Chunk(ids, 40) {
			var movies []webediaMovie
			mq := url.Values{"basic": {"false"}, "castingLimit": {"0"}, "ids": chunk}
			if err := s.GetJSON(api+"/movies", &movies, scrape.Query(mq)); err != nil {
				return nil, err
			}
			for _, m := range movies {
				titles[m.ID] = m.Title
				if m.Locale != nil && m.Locale.Title != "" {
					titles[m.ID] = m.Locale.Title
				}
				runtime[m.ID] = int(math.RoundToEven(m.Runtime / 60))
			}
		}
		var out []scrape.Show
		for _, mid := range ids {
			byDay := t.Schedule[mid]
			days := make([]string, 0, len(byDay))
			for d := range byDay {
				days = append(days, d)
			}
			slices.Sort(days)
			title, ok := titles[mid]
			if !ok {
				title = mid
			}
			for _, d := range days {
				for _, sh := range byDay[d] {
					out = append(out, scrape.Show{Film: title, Start: sh.StartsAt, Screen: webediaScreen(sh),
						Formats: scrape.Tags(webediaTags(sh.Tags), false), URL: webediaURL(sh), Duration: runtime[mid]})
				}
			}
		}
		return out, nil
	}
}

// webediaTags turns "Showtime.Format.SubtitledCinema"-style tags into raw labels.
func webediaTags(tags []string) []string {
	var raw []string
	for _, t := range tags {
		parts := strings.Split(t, ".")
		last := parts[len(parts)-1]
		if parts[0] == "Internal" && last != "XPlus" {
			continue
		}
		if len(parts) > 1 && parts[0] == "Auditorium" && parts[1] == "Comfort" {
			continue
		}
		label := scrape.Camel(last)
		if len(parts) > 1 && parts[1] == "Subtitle" {
			label += " subs"
		}
		raw = append(raw, label)
	}
	return raw
}

func webediaURL(sh webediaShow) string {
	for _, tk := range sh.Data.Ticketing {
		if tk.Provider == "default" && len(tk.URLs) > 0 {
			return tk.URLs[0]
		}
	}
	return ""
}

func webediaScreen(sh webediaShow) string {
	if sh.Screen == nil {
		return ""
	}
	n := sh.Screen.Name
	if n != "" && strings.Trim(n, "0123456789") == "" {
		return "Screen " + n
	}
	return n
}
