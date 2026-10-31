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
	a1PanelRe   = regexp.MustCompile(`^panel_?(\d+)$`)
	a1TimeRe    = regexp.MustCompile(`(\d{1,2}):(\d{2})`)
	a1CP1252Fix = strings.NewReplacer("\u0091", "'", "\u0092", "'", "\u0093", `"`, "\u0094", `"`, "\u0096", "-", "\u0097", "-")
)

// a1Icons maps the perf-type icon alt texts (Arc's "3D icon") to format tags; the rest
// (Hypersense, Kids Club, wheelchair) are dropped.
var a1Icons = map[string]string{"3D": "3D", "IMAX": "IMAX", "Infinity Vision": "Infinity Vision",
	"Arc Events": "Live/Event Cinema", "Parent & Baby": "Parent & Baby", "Sensory Screening": "Relaxed",
	"Silver Screen": "Silver Screening"}

// AdmitOne scrapes an Admit One cinema's /whatson page (Arc Cinemas, Parkway Barnsley, Lockworks).
// base is the site root without a trailing slash.
func AdmitOne(base string) scrape.Fetcher {
	return func(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
		r, err := s.GetOK(base + "/whatson")
		if err != nil {
			return nil, err
		}
		doc, err := r.Doc()
		if err != nil {
			return nil, err
		}
		return admitOneParse(doc, base, scrape.Day(time.Now().In(scrape.London)), from, to)
	}
}

// a1Day dates one day panel. The Arc template names it panel_<epoch of that day's midnight> (the
// +12h absorbs BST), Lockworks panel_<yyyymmdd>, Barnsley's older template panel<n> with the date only
// in the n-th tab ("Tomorrow", "Sun 01 Nov").
func a1Day(doc *goquery.Document, id string, today time.Time) (time.Time, bool, error) {
	m := a1PanelRe.FindStringSubmatch(id)
	if m == nil {
		return time.Time{}, false, nil
	}
	switch n, _ := strconv.ParseInt(m[1], 10, 64); {
	case len(m[1]) == 8:
		d, err := time.Parse("20060102", m[1])
		return d, err == nil, err
	case len(m[1]) >= 9:
		return scrape.Day(time.Unix(n, 0).UTC().Add(12 * time.Hour)), true, nil
	}
	label := scrape.TextS(doc.Find(`button.tab[data-target="` + m[1] + `"]`).First())
	if l := strings.ToLower(label); l == "today" || l == "tomorrow" {
		return today.AddDate(0, 0, map[string]int{"today": 0, "tomorrow": 1}[l]), true, nil
	}
	d, ok, err := labelDate(label, today)
	if err == nil && !ok {
		err = fmt.Errorf("Admit One: unreadable date tab %q", label)
	}
	return d, ok, err
}

// admitOneParse reads the day panels: each film block has the title (h2), "Running time: 98 mins" and a
// button per performance. Arc's buttons read "13:45 - 17:15", but that end is start + runtime without
// the ads (the page's own runtime says so), so it is ignored. Mobile and desktop copies of a block
// (Lockworks) share a booking URL and are dropped.
func admitOneParse(doc *goquery.Document, base string, today, from, to time.Time) ([]scrape.Show, error) {
	seen := map[string]bool{}
	var out []scrape.Show
	var ferr error
	doc.Find("div[id^=panel]").EachWithBreak(func(_ int, panel *goquery.Selection) bool {
		day, ok, err := a1Day(doc, scrape.Attr(panel, "id"), today)
		if err != nil {
			ferr = err
			return false
		}
		if !ok || !inRange(day, from, to) {
			return true
		}
		panel.Find("a.perfButton").Each(func(_ int, b *goquery.Selection) {
			u := urljoin(base+"/", scrape.Attr(b, "href"))
			tm := a1TimeRe.FindStringSubmatch(scrape.TextS(b))
			block := b
			for block.Find("h2 a").Length() == 0 && !block.Is("div[id^=panel]") {
				block = block.Parent()
			}
			title := a1CP1252Fix.Replace(scrape.TextS(block.Find("h2 a").First()))
			if tm == nil || title == "" || seen[u] {
				return
			}
			seen[u] = true
			h, _ := strconv.Atoi(tm[1])
			fm := []string{}
			b.Find("img[alt$=' icon']").Each(func(_ int, im *goquery.Selection) {
				if t := a1Icons[strings.TrimSuffix(scrape.Attr(im, "alt"), " icon")]; t != "" {
					fm = append(fm, t)
				}
			})
			if b.Find("i.a1-event-subtitles").Length() > 0 {
				fm = append(fm, "Subtitled")
			}
			fm = append(fm, titleTags(title)...)
			out = append(out, scrape.Show{
				Film:     title,
				Start:    fmt.Sprintf("%sT%02d:%s", day.Format(scrape.DateFmt), h, tm[2]),
				Formats:  scrape.Uniq(fm),
				URL:      u,
				Duration: scrape.Mins(scrape.TextS(block.Find("p:contains('Running time')").First())),
			})
		})
		return true
	})
	if ferr != nil {
		return nil, ferr
	}
	slices.SortStableFunc(out, func(x, y scrape.Show) int { return strings.Compare(x.Start, y.Start) })
	return out, nil
}
