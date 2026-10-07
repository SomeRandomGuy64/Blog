package chains

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"fazon.nexus/cinema/internal/scrape"
)

var (
	reelDayClass = regexp.MustCompile(`^nowshowingslider_(\d{8})$`)
	reelRuntime  = regexp.MustCompile(`Running time:\s*(\d+)\s*min`)
)

// Reel scrapes one Reel cinema's what's-on page.
func Reel(town string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		r, err := s.GetOK("https://reel-cinemas.co.uk/" + town + "/whatson/")
		if err != nil {
			return nil, err
		}
		doc, err := r.Doc()
		if err != nil {
			return nil, err
		}
		var out []scrape.Show
		doc.Find("div.nowshowing_film_box_outer").Each(func(_ int, box *goquery.Selection) {
			day := ""
			for _, c := range strings.Fields(scrape.Attr(box, "class")) {
				if m := reelDayClass.FindStringSubmatch(c); m != nil {
					day = m[1]
					break
				}
			}
			title := box.Find(".reel_title h2").First()
			if day == "" || title.Length() == 0 {
				return
			}
			dur := 0
			if m := reelRuntime.FindStringSubmatch(scrape.TextS(box)); m != nil {
				dur, _ = strconv.Atoi(m[1])
			}
			seen := map[string]bool{}
			box.Find("a.show_time_in").Each(func(_ int, a *goquery.Selection) {
				href := scrape.Attr(a, "href")
				if seen[href] {
					return
				}
				seen[href] = true
				when, err := time.Parse("2006010215:04", day+scrape.Text(a, "", true))
				if err != nil {
					return
				}
				out = append(out, scrape.Show{Film: scrape.Text(title, "", true), Start: when.Format(scrape.TimeFmt),
					Formats: []string{}, URL: href, Duration: dur})
			})
		})
		return out, nil
	}
}
