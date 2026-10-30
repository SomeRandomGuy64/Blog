package venues

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

// gfData is the part of a Glitterfish "miniguide" that matters: films, each with a day list of
// formats and their sessions.
type gfData struct {
	Schedule []struct {
		ProgrammeType int
		Title         string
		Url           string
		Runtime       string // "1h 49m"
		Dates         []struct {
			Key     string // yyyymmdd
			Formats []struct {
				Format   string
				Sessions []struct {
					BOID        string
					Display     string // "10.50"
					Url         string
					Collections []struct{ Title string }
				}
			}
		}
	}
}

// Glitterfish scrapes a Glitterfish site's miniguide (Parkway Beverley, Cleethorpes, Louth and
// Workington): JSON assigned to a JS variable. base is the site root without a trailing slash.
func Glitterfish(base string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		r, err := s.GetOK(base + "/resource/services/miniguide/data.ashx")
		if err != nil {
			return nil, err
		}
		return glitterfishParse(r.Body, base, from, to)
	}
}

func glitterfishParse(body []byte, base string, from, to time.Time) ([]scrape.Show, error) {
	i, j := bytes.IndexByte(body, '{'), bytes.LastIndexByte(body, '}')
	var d gfData
	if i < 0 || j < i {
		return nil, fmt.Errorf("Glitterfish %s: no miniguide data", base)
	}
	if err := json.Unmarshal(body[i:j+1], &d); err != nil {
		return nil, fmt.Errorf("Glitterfish %s: %w", base, err)
	}
	seen := map[string]bool{}
	var out []scrape.Show
	for _, f := range d.Schedule {
		var tags0 []string                                // tags that belong to the film, not a session
		if f.ProgrammeType == 3 || f.ProgrammeType == 4 { // Event Cinema / Live on Stage
			tags0 = append(tags0, "Live/Event Cinema")
		}
		tags0 = append(tags0, fmtTags(f.Title)...)
		for _, day := range f.Dates {
			dt, err := time.Parse("20060102", day.Key)
			if err != nil {
				return nil, fmt.Errorf("Glitterfish %s: bad day %q", base, day.Key)
			}
			if !inRange(dt, from, to) {
				continue
			}
			for _, fm := range day.Formats {
				for _, se := range fm.Sessions {
					hhmm := strings.Replace(se.Display, ".", ":", 1)
					if _, err := time.Parse("15:04", hhmm); err != nil || seen[se.BOID] {
						continue
					}
					seen[se.BOID] = true
					tags := slices.Clone(tags0)
					if fm.Format == "3D" {
						tags = append(tags, "3D")
					}
					for _, c := range se.Collections {
						if c.Title == "Senior Screen" {
							tags = append(tags, "Silver Screening")
						}
						tags = append(tags, fmtTags(c.Title)...)
					}
					out = append(out, scrape.Show{
						Film:     f.Title,
						Start:    dt.Format(scrape.DateFmt) + "T" + fmt.Sprintf("%05s", hhmm),
						Formats:  scrape.Uniq(tags),
						URL:      se.Url,
						Duration: scrape.Mins(f.Runtime),
					})
				}
			}
		}
	}
	slices.SortStableFunc(out, func(x, y scrape.Show) int { return strings.Compare(x.Start, y.Start) })
	return out, nil
}
