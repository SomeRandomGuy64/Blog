package venues

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

// Savoy Systems CCM sites embed the whole programme as `var Events = {...}` in /WhatsOn.

func Cultplex(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	return savoy(s, "https://cultplex.co.uk/Cultplex.dll/WhatsOn", from, to)
}

func HebdenBridge(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	return savoy(s, "https://hebdenbridgepicturehouse.co.uk/HebdenBridgePictureHouse.dll/WhatsOn", from, to)
}

func PlazaCrosby(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	return savoy(s, "https://plazacinema.org.uk/PlazaCinemaLiverpool.dll/WhatsOn", from, to)
}

var (
	savoyEventsRe = regexp.MustCompile(`var Events\s*=\s*`)
	// Per-site legend of Y/N performance flags: <span class="tag">AD</span> Audio Description
	savoyLegendRe = regexp.MustCompile(`<span class="tag">([A-Z0-9]+)</span>\s*([^<]+)<`)
)

type savoyEvent struct {
	Title, TypeDescription, Genre, RunningTime, URL any
	Performances                                    []map[string]any
}

func savoy(s *scrape.Session, whatsOn string, from, to time.Time) ([]scrape.Show, error) {
	r, err := s.GetOK(whatsOn)
	if err != nil {
		return nil, err
	}
	page := r.Text()
	loc := savoyEventsRe.FindStringIndex(page)
	if loc == nil {
		return nil, errors.New("Savoy: `var Events` blob not found")
	}
	var data struct{ Events []savoyEvent }
	dec := json.NewDecoder(strings.NewReader(page[loc[1]:]))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("Savoy: decode Events: %w", err)
	}
	legend := map[string]string{}
	for _, m := range savoyLegendRe.FindAllStringSubmatch(page, -1) {
		legend[m[1]] = strings.TrimSpace(html.UnescapeString(m[2]))
	}
	base := whatsOn[:strings.LastIndex(whatsOn, "/")+1]
	var out []scrape.Show
	for _, ev := range data.Events {
		title := strings.TrimSpace(html.UnescapeString(pystr(ev.Title)))
		if title == "" {
			continue
		}
		for _, p := range ev.Performances {
			st := pystr(p["StartTime"])
			if n := 4 - len(st); n > 0 { // str.zfill(4)
				st = strings.Repeat("0", n) + st
			}
			t, err := time.Parse("2006-1-2 1504", pystr(p["StartDate"])+" "+st)
			if err != nil || !inRange(scrape.Day(t), from, to) {
				continue
			}
			texts := []string{title, pystr(ev.TypeDescription), pystr(ev.Genre), pystr(p["Notes"])}
			for k, v := range p {
				if l, ok := legend[k]; ok && v == "Y" {
					texts = append(texts, l)
				}
			}
			u := pystr(p["URL"])
			if u != "" && !strings.HasPrefix(u, "http") {
				u = base + u
			}
			if u == "" {
				u = pystr(ev.URL)
			}
			out = append(out, scrape.Show{
				Film:     title,
				Start:    t.Format(scrape.TimeFmt),
				Screen:   strings.TrimSpace(pystr(p["AuditoriumName"])),
				Formats:  svFormats(texts...),
				URL:      u,
				Duration: scrape.ToInt(ev.RunningTime),
			})
		}
	}
	return out, nil
}

// pystr is str(x or "") for decoded JSON scalars.
func pystr(x any) string {
	switch v := x.(type) {
	case string:
		return v
	case json.Number:
		if v == "0" {
			return ""
		}
		return v.String()
	case bool:
		if v {
			return "True"
		}
	}
	return ""
}
