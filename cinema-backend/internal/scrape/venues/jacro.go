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

var jacroTimeRe = regexp.MustCompile(`(?i)(\d{1,2}):(\d{2})\s*(am|pm)?`)

// jacroRelRe matches a relative date label. today is the fetch date in London, not the window start
// (from may be later), so it is passed separately.
var jacroRelRe = regexp.MustCompile(`(?i)^\s*(today|tomorrow)\b`)

// jacroTags maps Prince Charles' tag classes (<span class="tag 35mm">) to format tags.
var jacroTags = map[string]string{"35mm": "35mm", "70mm": "70mm", "4k": "4K", "sub": "Subtitled", "hoh": "Subtitled",
	"sing-along": "Sing-along", "q-a": "Q&A", "preview": "Preview"}

// Jacro scrapes a Jacro cinema's listings page (Prince Charles, Cottage Road, Cinemac): one block per
// film with a booking link (/booknow/<id>) per performance, under a date label that has no year.
func Jacro(page string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		r, err := s.GetOK(page)
		if err != nil {
			return nil, err
		}
		doc, err := r.Doc()
		if err != nil {
			return nil, err
		}
		return jacroParse(doc, page, scrape.Day(time.Now().In(scrape.London)), from, to)
	}
}

// jacroParse walks each film block in document order: a date label (the three sites mark it up
// differently) sets the day for the booking links that follow. A link can appear twice on a page
// (Cinemac repeats each in a hidden category list), so they are de-duplicated.
func jacroParse(doc *goquery.Document, page string, today, from, to time.Time) ([]scrape.Show, error) {
	seen := map[string]bool{}
	var out []scrape.Show
	var ferr error
	doc.Find(".jacro-event, .movie-tabs").EachWithBreak(func(_ int, film *goquery.Selection) bool {
		title := scrape.TextS(film.Find("a.liveeventtitle, h3 a").First())
		if title == "" {
			return true
		}
		rt := film.Find(".running-time").First()
		if rt.Length() == 0 {
			rt = film.Find("i.fa-film").First().Parent()
		}
		dur := scrape.Mins(scrape.TextS(rt))
		filmTags := scrape.Uniq(append(titleTags(title), svFormats(title)...))
		var day time.Time
		film.Find(`div.heading, div.neworderpf > span:first-of-type, ` +
			`div.jacro-date-showtime-list > div:not(.perfbtns):not(.neworderpf), a[href*="booknow/"]`).EachWithBreak(
			func(_ int, el *goquery.Selection) bool {
				if el.Is("a") {
					u := urljoin(page, scrape.Attr(el, "href"))
					m := jacroTimeRe.FindStringSubmatch(scrape.TextS(el))
					if m == nil && el.Find(".time").Length() > 0 {
						m = jacroTimeRe.FindStringSubmatch(scrape.TextS(el.Find(".time")))
					}
					if day.IsZero() || m == nil || seen[u] || !inRange(day, from, to) {
						return true
					}
					seen[u] = true
					h, _ := strconv.Atoi(m[1])
					switch strings.ToLower(m[3]) {
					case "pm":
						h = h%12 + 12
					case "am":
						h %= 12
					}
					tags := slices.Clone(filmTags)
					for _, c := range classes(el.Closest("li")) {
						tags = append(tags, jacroTags[c])
					}
					el.Parent().Find(".tag").Each(func(_ int, t *goquery.Selection) { // Prince Charles' tags
						for _, c := range classes(t) {
							tags = append(tags, jacroTags[c])
						}
					})
					out = append(out, scrape.Show{
						Film:     title,
						Start:    fmt.Sprintf("%sT%02d:%s", day.Format(scrape.DateFmt), h, m[2]),
						Formats:  scrape.Uniq(slices.DeleteFunc(tags, func(t string) bool { return t == "" })),
						URL:      u,
						Duration: dur,
					})
					return true
				}
				label := scrape.TextS(el)
				if m := jacroRelRe.FindStringSubmatch(label); m != nil { // "Today" / "Tomorrow" headings
					day = today.AddDate(0, 0, map[string]int{"today": 0, "tomorrow": 1}[strings.ToLower(m[1])])
					return true
				}
				d, ok, err := labelDate(label, today)
				if err != nil {
					ferr = err
					return false
				}
				if ok {
					day = d
				}
				return true
			})
		return ferr == nil
	})
	if ferr != nil {
		return nil, ferr
	}
	slices.SortStableFunc(out, func(x, y scrape.Show) int { return strings.Compare(x.Start, y.Start) })
	return out, nil
}
