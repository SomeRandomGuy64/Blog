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
	months = []string{"january", "february", "march", "april", "may", "june", "july", "august",
		"september", "october", "november", "december"}
	spDateRe = regexp.MustCompile(`(?i)(?:(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\s+)?(\d{1,2})` +
		`(?:st|nd|rd|th)?\s+(` + strings.Join(months, "|") + `)(?:\s+(\d{4}))?`)
	spTimeRe  = regexp.MustCompile(`(?i)(\d{1,2})(?:[.:](\d{2}))?\s*(am|pm)`)
	spLiveRe  = regexp.MustCompile(`(?i)live|orchestra|band|vox`)
	spTitleRe = regexp.MustCompile(`^(?:[\p{L}\p{N}_' :]+?\s)?Film\s*[:-]\s*`)
)

// spFilm strips Stockport Plaza's "Classic Film: " / "Film - " style prefixes.
func spFilm(title string) string { return strings.TrimSpace(spTitleRe.ReplaceAllString(title, "")) }

// StockportPlaza scrapes the WordPress "type/film" listing: one card per film with a free-text
// date like "Monday 26th April at 1.00pm & 7.00pm and Tuesday 27th April at 1.00pm" (usually no year).
func StockportPlaza(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	r, err := s.GetOK("https://stockportplaza.co.uk/whats-on/type/film/")
	if err != nil {
		return nil, err
	}
	doc, err := r.Doc()
	if err != nil {
		return nil, err
	}
	var out []scrape.Show
	var ferr error
	doc.Find("div.event").EachWithBreak(func(_ int, ev *goquery.Selection) bool {
		t, dnode := ev.Find("p.title a").First(), ev.Find("p.date").First()
		if t.Length() == 0 || dnode.Length() == 0 {
			return true
		}
		var cats []string
		ev.Find(".filterRow a").Each(func(_ int, a *goquery.Selection) { cats = append(cats, scrape.Text(a, "", true)) })
		if !slices.Contains(cats, "Film") {
			return true
		}
		title := scrape.TextS(t)
		var tix []string
		ev.Find(".ctaContainer a").Each(func(_ int, a *goquery.Selection) {
			if strings.Contains(scrape.Text(a, "", false), "Tickets") {
				tix = append(tix, strings.TrimSpace(scrape.Attr(a, "href")))
			}
		})
		u := scrape.Attr(t, "href")
		if len(tix) > 0 {
			u = tix[0]
		}
		text := scrape.TextS(dnode)
		ms := spDateRe.FindAllStringSubmatchIndex(text, -1)
		for i, m := range ms {
			grp := func(g int) string {
				if m[2*g] < 0 {
					return ""
				}
				return text[m[2*g]:m[2*g+1]]
			}
			dd, _ := strconv.Atoi(grp(2))
			mon := time.Month(slices.Index(months, strings.ToLower(grp(3))) + 1)
			var d time.Time
			if yr := grp(4); yr != "" {
				y, _ := strconv.Atoi(yr)
				var ok bool
				if d, ok = date(y, mon, dd); !ok {
					ferr = fmt.Errorf("Stockport Plaza: bad date %q", text[m[0]:m[1]])
					return false
				}
			} else if d, err = guessYear(dd, mon, strings.ToLower(grp(1)), from); err != nil {
				ferr = err
				return false
			}
			end := len(text)
			if i+1 < len(ms) {
				end = ms[i+1][0]
			}
			for _, tm := range spTimeRe.FindAllStringSubmatch(text[m[1]:end], -1) {
				h, _ := strconv.Atoi(tm[1])
				h %= 12
				if strings.EqualFold(tm[3], "pm") {
					h += 12
				}
				if !inRange(d, from, to) {
					continue
				}
				fm := fmtTags(title)
				if slices.Contains(cats, "Music") && !slices.Contains(fm, "Live/Event Cinema") && spLiveRe.MatchString(title) {
					fm = append(fm, "Live/Event Cinema") // film with live score
				}
				mi := tm[2]
				if mi == "" {
					mi = "00"
				}
				out = append(out, scrape.Show{
					Film:    spFilm(title),
					Start:   fmt.Sprintf("%sT%02d:%s", d.Format(scrape.DateFmt), h, mi),
					Formats: fm,
					URL:     u,
				})
			}
		}
		return true
	})
	if ferr != nil {
		return nil, ferr
	}
	return out, nil
}
