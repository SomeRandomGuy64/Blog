package venues

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

var (
	pvTimeRe = regexp.MustCompile(`^(\d{1,2})[.:](\d{2})`)
	pvIMAXRe = regexp.MustCompile(`^IMAX(?: 3D)?:\s*`)
)

// Pictureville queries the Drupal search API behind scienceandmediamuseum.org.uk/cinema/listings
// one day at a time (it returns max 36 occurrences per call and ignores paging params).
func Pictureville(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	const base = "https://www.scienceandmediamuseum.org.uk"
	var out []scrape.Show
	for _, day := range scrape.Days(from, scrape.DailyTo(from, to)) {
		ds := day.Format(scrape.DateFmt)
		var res struct {
			Items []struct {
				Bundle string `json:"bundle"`
				Markup string `json:"markup"`
			} `json:"items"`
		}
		err := s.GetJSON(base+"/api/listing/31281", &res, scrape.Query(url.Values{
			"filter[when][path]": {"whats_on_when"}, "filter[when][value][]": {ds + "TO" + ds}}))
		if err != nil {
			return nil, err
		}
		for _, it := range res.Items {
			if it.Bundle != "occurrence" {
				continue
			}
			b, err := scrape.Doc([]byte(it.Markup))
			if err != nil {
				return nil, err
			}
			titleEl, dateEl, timeEl := b.Find(".c-card-list__title").First(), b.Find(".c-card-list__date").First(),
				b.Find(".c-card-list__time").First()
			if titleEl.Length() == 0 || dateEl.Length() == 0 || timeEl.Length() == 0 {
				return nil, fmt.Errorf("Pictureville: occurrence card missing title/date/time")
			}
			title := scrape.TextS(titleEl)
			d, err := time.Parse("2 January 2006", scrape.Text(dateEl, "", true))
			if err != nil {
				return nil, err
			}
			tm := pvTimeRe.FindStringSubmatch(scrape.Text(timeEl, "", true))
			if tm == nil {
				return nil, fmt.Errorf("Pictureville: bad time %q", scrape.Text(timeEl, "", true))
			}
			h, _ := strconv.Atoi(tm[1])
			cat := "" // Film / IMAX / IMAX 3D / Event cinema / Special event
			if c := b.Find(".c-card-list__meta-item").First(); c.Length() > 0 {
				cat = scrape.Text(c, "", true)
			}
			fmts := fmtTags(title + " | " + cat)
			screen := ""
			if slices.Contains(fmts, "IMAX") {
				screen = "IMAX"
			}
			u := ""
			if a := b.Find("a[href]").First(); a.Length() > 0 {
				if u = scrape.Attr(a, "href"); strings.HasPrefix(u, "/") {
					u = base + u
				}
			}
			out = append(out, scrape.Show{
				Film:    pvIMAXRe.ReplaceAllString(title, ""),
				Start:   fmt.Sprintf("%sT%02d:%s", d.Format(scrape.DateFmt), h, tm[2]),
				Screen:  screen,
				Formats: fmts,
				URL:     u,
			})
		}
	}
	return out, nil
}
