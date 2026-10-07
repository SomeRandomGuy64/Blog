package chains

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

// Light fetches one The Light cinema from its miniguide feed (JSON wrapped in JS).
func Light(sub string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		base := "https://" + sub + ".thelight.co.uk"
		r, err := s.GetOK(base + "/resource/services/miniguide/data.ashx")
		if err != nil {
			return nil, err
		}
		i, k := bytes.IndexByte(r.Body, '{'), bytes.LastIndexByte(r.Body, '}')
		if i < 0 || k < i {
			return nil, fmt.Errorf("light: no JSON in miniguide feed")
		}
		var data struct {
			Schedule []struct {
				Title   string `json:"Title"`
				URL     string `json:"Url"`
				Runtime string `json:"Runtime"` // "1h 57m"
				Dates   []struct {
					Key      string `json:"Key"` // 20261007
					Sessions []struct {
						Sort          flexStr `json:"Sort"` // 1040
						FormatDisplay string  `json:"FormatDisplay"`
						Collections   []struct {
							Title string `json:"Title"`
						} `json:"Collections"`
					} `json:"Sessions"`
				} `json:"Dates"`
			} `json:"Schedule"`
		}
		if err := json.Unmarshal(r.Body[i:k+1], &data); err != nil {
			return nil, fmt.Errorf("light: %w", err)
		}
		var out []scrape.Show
		for _, f := range data.Schedule {
			for _, d := range f.Dates {
				for _, se := range d.Sessions {
					sort := string(se.Sort)
					if len(sort) < 4 {
						sort = strings.Repeat("0", 4-len(sort)) + sort
					}
					when, err := time.Parse("200601021504", d.Key+sort)
					if err != nil {
						return nil, fmt.Errorf("light: bad session time %q: %w", d.Key+sort, err)
					}
					var raw []string
					for _, c := range se.Collections {
						raw = append(raw, c.Title)
					}
					fmts := append(scrape.Tags([]string{se.FormatDisplay}, false), scrape.Tags(raw, true)...) // 2D / 3D / Light MAX ...
					out = append(out, scrape.Show{Film: f.Title, Start: when.Format(scrape.TimeFmt), Formats: fmts,
						URL: base + f.URL, Duration: scrape.Mins(f.Runtime)})
				}
			}
		}
		return out, nil
	}
}
