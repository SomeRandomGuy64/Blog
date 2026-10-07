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

const penistoneBase = "https://penistoneparamount.co.uk/"

var (
	penDayRe   = regexp.MustCompile(`(\d{1,2})\s+([A-Za-z]{3})`)
	penEventRe = regexp.MustCompile(`event/(\d+)`)
	penRTRe    = regexp.MustCompile(`Running time:\s*(\d+)\s*min`)
	penTimeRe  = regexp.MustCompile(`(\d{1,2}):(\d{2})`)
)

// penRows is an insertion-ordered map of performances keyed by (event, day, time); a
// re-inserted key keeps its first position (like a Python dict).
type penRows struct {
	idx  map[string]int
	rows []scrape.Show
}

func (p *penRows) put(key string, row scrape.Show) {
	if i, ok := p.idx[key]; ok {
		p.rows[i] = row
		return
	}
	p.idx[key] = len(p.rows)
	p.rows = append(p.rows, row)
}

func penKey(ev string, day time.Time, hhmm string) string {
	return ev + "|" + day.Format(scrape.DateFmt) + "|" + hhmm
}

// penHHMM returns "HH:MM" for the first h:mm in s.
func penHHMM(s string) (string, bool) {
	m := penTimeRe.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	h, _ := strconv.Atoi(m[1])
	return fmt.Sprintf("%02d:%s", h, m[2]), true
}

// penDay dates a "Thu 08 Oct"-style label relative to today.
func penDay(m []string, today time.Time) (time.Time, error) {
	mon, err := monthAbbr(m[2])
	if err != nil {
		return time.Time{}, err
	}
	d, _ := strconv.Atoi(m[1])
	return nearestYear(mon, d, today)
}

// penistoneParse reads the /whatson day-tab page.
func penistoneParse(doc *goquery.Document, from, to, today time.Time, rows *penRows) error {
	labels := map[string]string{}
	doc.Find("#scheduleTabs button[data-target]").Each(func(_ int, b *goquery.Selection) {
		labels[scrape.Attr(b, "data-target")] = scrape.TextS(b)
	})
	var err error
	doc.Find(`div.tab-content[id^="panel"]`).EachWithBreak(func(_ int, panel *goquery.Selection) bool {
		label := strings.ToLower(labels[scrape.Attr(panel, "id")[5:]])
		var day time.Time
		switch label {
		case "today":
			day = today
		case "tomorrow":
			day = today.AddDate(0, 0, 1)
		default:
			m := penDayRe.FindStringSubmatch(label)
			if m == nil {
				return true
			}
			if day, err = penDay(m, today); err != nil {
				return false
			}
		}
		if !inRange(day, from, to) {
			return true
		}
		panel.Find("h2").Each(func(_ int, h2 *goquery.Selection) {
			a := h2.Find("a").First()
			card := h2.ParentsFiltered("div.grid-flow-col").First()
			if card.Length() == 0 {
				card = h2.Parent().Parent().Parent()
			}
			if a.Length() == 0 || card.Length() == 0 {
				return
			}
			href := scrape.Attr(a, "href")
			key := scrape.Text(a, "", true)
			if m := penEventRe.FindStringSubmatch(href); m != nil {
				key = m[1]
			}
			evURL := urljoin(penistoneBase, href)
			dur := 0
			if m := penRTRe.FindStringSubmatch(scrape.TextS(card)); m != nil {
				dur, _ = strconv.Atoi(m[1])
			}
			film := strings.Join(strings.Fields(strings.ReplaceAll(scrape.Text(a, " ", false), "\u0096", "-")), " ")
			card.Find(".perfButton").Each(func(_ int, b *goquery.Selection) {
				hhmm, ok := penHHMM(scrape.Text(b, "", false))
				if !ok {
					return
				}
				u := scrape.Attr(b, "href")
				if u == "" {
					u = evURL
				}
				rows.put(penKey(key, day, hhmm), scrape.Show{
					Film:     film,
					Start:    day.Format(scrape.DateFmt) + "T" + hhmm,
					Formats:  []string{},
					URL:      u,
					Duration: dur,
				})
			})
		})
		return true
	})
	return err
}

// Penistone scrapes Penistone Paramount (Admit-One), skipping its live gigs.
func Penistone(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	today := scrape.Day(time.Now().In(scrape.London))
	r, err := s.GetOK(penistoneBase + "whatson")
	if err != nil {
		return nil, err
	}
	doc, err := r.Doc()
	if err != nil {
		return nil, err
	}
	rows := &penRows{idx: map[string]int{}}
	if err := penistoneParse(doc, from, to, today, rows); err != nil {
		return nil, err
	}
	// Category listings flag performances (cards: <p>Thu 08 Oct</p><p>10:30</p> Book Now).
	for _, c := range [][2]string{{"subtitled", "Subtitled"}, {"relaxed", "Relaxed"},
		{"event", "Live/Event Cinema"}, {"live", "Live Performance"}} {
		rc, err := s.Get(penistoneBase + "whatson/" + c[0])
		if err != nil {
			return nil, err
		}
		if rc.Status != 200 {
			continue
		}
		cdoc, err := rc.Doc()
		if err != nil {
			return nil, err
		}
		var ferr error
		cdoc.Find("#panels h2").EachWithBreak(func(_ int, h2 *goquery.Selection) bool {
			a := h2.Find("a").First()
			box := h2.ParentsFiltered("div.bg-secondary").First()
			if a.Length() == 0 || box.Length() == 0 {
				return true
			}
			ev := penEventRe.FindStringSubmatch(scrape.Attr(a, "href"))
			if ev == nil {
				return true
			}
			box.Find("div.justify-between.w-full").EachWithBreak(func(_ int, row *goquery.Selection) bool {
				var ps []string
				row.Find("p").Each(func(_ int, p *goquery.Selection) { ps = append(ps, scrape.TextS(p)) })
				var dm []string
				var hhmm string
				var ok bool
				if len(ps) > 0 {
					dm = penDayRe.FindStringSubmatch(ps[0])
				}
				if len(ps) > 1 {
					hhmm, ok = penHHMM(ps[1])
				}
				if dm == nil || !ok {
					return true
				}
				day, err := penDay(dm, today)
				if err != nil {
					ferr = err
					return false
				}
				if i, hit := rows.idx[penKey(ev[1], day, hhmm)]; hit && !slices.Contains(rows.rows[i].Formats, c[1]) {
					rows.rows[i].Formats = append(rows.rows[i].Formats, c[1])
				}
				return true
			})
			return ferr == nil
		})
		if ferr != nil {
			return nil, ferr
		}
	}
	var out []scrape.Show
	for _, row := range rows.rows {
		for _, t := range svFormats(row.Film) {
			if !slices.Contains(row.Formats, t) {
				row.Formats = append(row.Formats, t)
			}
		}
		if !slices.Contains(row.Formats, "Live Performance") {
			out = append(out, row)
		}
	}
	slices.SortStableFunc(out, func(x, y scrape.Show) int { return strings.Compare(x.Start, y.Start) })
	return out, nil
}
