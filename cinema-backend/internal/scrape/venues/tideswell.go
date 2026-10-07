package venues

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/PuerkitoBio/goquery"

	"fazon.nexus/cinema/internal/scrape"
)

var (
	tdWdayRe = regexp.MustCompile(`^[\p{L}\p{N}_]+\.?\s+`)
	tdTimeRe = regexp.MustCompile(`(\d{1,2}):(\d{2})`)
	tdFilmRe = regexp.MustCompile(`(?i)film`)
)

// Tideswell scrapes WordPress + Modern Events Calendar: /all-screenings/ lists the next ~6
// forthcoming events (films + NT/ROH/Met live broadcasts) with times, followed by past ones
// (no times).
func Tideswell(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	r, err := s.GetOK("https://tideswellcinema.com/all-screenings/")
	if err != nil {
		return nil, err
	}
	doc, err := r.Doc()
	if err != nil {
		return nil, err
	}
	var out []scrape.Show
	var ferr error
	doc.Find("article.mec-event-article").EachWithBreak(func(_ int, art *goquery.Selection) bool {
		dl, st, a := art.Find(".mec-start-date-label").First(), art.Find(".mec-start-time").First(),
			art.Find(".mec-event-title a").First()
		if dl.Length() == 0 || st.Length() == 0 || a.Length() == 0 {
			return true // past performances carry no time
		}
		d, err := time.Parse("2 Jan, 2006", tdWdayRe.ReplaceAllString(scrape.Text(dl, "", true), ""))
		if err != nil {
			ferr = fmt.Errorf("Tideswell: %w", err)
			return false
		}
		tm := tdTimeRe.FindStringSubmatch(scrape.Text(st, "", false))
		if tm == nil {
			ferr = fmt.Errorf("Tideswell: no time in %q", scrape.TextS(st))
			return false
		}
		if !inRange(d, from, to) {
			return true
		}
		lab := ""
		if l := art.Find(".mec-event-label").First(); l.Length() > 0 {
			lab = scrape.TextS(l)
		}
		var fmts []string
		film := scrape.TextS(a)
		if lab != "" && !tdFilmRe.MatchString(lab) {
			fmts = []string{"Live/Event Cinema"}
			film = lab + ": " + film // e.g. "Met Opera: Macbeth"
		}
		h, _ := strconv.Atoi(tm[1])
		out = append(out, scrape.Show{
			Film:    film,
			Start:   fmt.Sprintf("%sT%02d:%s", d.Format(scrape.DateFmt), h, tm[2]),
			Formats: fmts,
			URL:     scrape.Attr(a, "href"),
		})
		return true
	})
	if ferr != nil {
		return nil, ferr
	}
	return out, nil
}
