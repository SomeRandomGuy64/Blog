package venues

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"fazon.nexus/cinema/internal/scrape"
)

var (
	skiptonMinsRe = regexp.MustCompile(`(\d+)\s*mins`)
	skiptonDateRe = regexp.MustCompile(`(\d{1,2})\s*(?:st|nd|rd|th)?\s+([A-Za-z]{3})`)
	skiptonTimeRe = regexp.MustCompile(`^(\d{1,2}):(\d{2})`)
)

// Taposapp scrapes a taposapp site's programme pages (Plaza Skipton, Picture House Keighley, Rex Elland);
// base is the site root with a trailing slash.
func Taposapp(base string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		return taposapp(s, base, from, to)
	}
}

func taposapp(s *scrape.Session, base string, from, to time.Time) ([]scrape.Show, error) {
	seen, rt := map[string]bool{}, map[string]int{}
	var out []scrape.Show
	for _, path := range []string{"", "coming-soon", "event-cinema"} {
		r, err := s.GetOK(base + path)
		if err != nil {
			return nil, err
		}
		doc, err := r.Doc()
		if err != nil {
			return nil, err
		}
		doc.Find("article.hero-slide").Each(func(_ int, slide *goquery.Selection) { // only featured films carry "101 mins"
			a := slide.Find("a[href^='/film/']").First()
			m := skiptonMinsRe.FindStringSubmatch(scrape.Text(slide, " ", false))
			if a.Length() > 0 && m != nil {
				rt[scrape.Attr(a, "href")], _ = strconv.Atoi(m[1])
			}
		})
		var ferr error
		doc.Find("div.programme-film").EachWithBreak(func(_ int, film *goquery.Selection) bool {
			a := film.Find(".programme-film-title a").First()
			if a.Length() == 0 {
				return true
			}
			title := scrape.TextS(a)
			film.Find(".programme-showtime-date").EachWithBreak(func(_ int, d *goquery.Selection) bool {
				m := skiptonDateRe.FindStringSubmatch(scrape.TextS(d))
				times := d.NextAllFiltered("div.programme-performance-times").First()
				if m == nil || times.Length() == 0 {
					return true
				}
				mon, err := monthAbbr(m[2])
				if err != nil {
					ferr = err
					return false
				}
				dd, _ := strconv.Atoi(m[1])
				day, err := nearestYear(mon, dd, from)
				if err != nil {
					ferr = err
					return false
				}
				if !inRange(day, from, to) {
					return true
				}
				times.Find("a.programme-performance-time").Each(func(_ int, t *goquery.Selection) {
					txt := scrape.TextS(t) // e.g. "19:55 = Subtitled"
					tm := skiptonTimeRe.FindStringSubmatchIndex(txt)
					u := urljoin(base, scrape.Attr(t, "href"))
					if tm == nil || seen[u] {
						return
					}
					seen[u] = true
					h, _ := strconv.Atoi(txt[tm[2]:tm[3]])
					note := txt[tm[1]:] + " " + strings.Join(classes(t), " ")
					out = append(out, scrape.Show{
						Film:     title,
						Start:    fmt.Sprintf("%sT%02d:%s", day.Format(scrape.DateFmt), h, txt[tm[4]:tm[5]]),
						Formats:  svFormats(strings.ReplaceAll(note, "-", " "), title),
						URL:      u,
						Duration: rt[scrape.Attr(a, "href")],
					})
				})
				return true
			})
			return ferr == nil
		})
		if ferr != nil {
			return nil, ferr
		}
	}
	slices.SortStableFunc(out, func(x, y scrape.Show) int { return strings.Compare(x.Start, y.Start) })
	return out, nil
}
