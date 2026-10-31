// Package venues holds the independent cinemas: Savoy CCM sites, the taposapp (Plaza Skipton), Admit One
// (Arc, Parkway Barnsley, Lockworks), Glitterfish (Parkway) and Jacro (Prince Charles, Cottage Road,
// Cinemac) platforms, and one-offs: Penistone Paramount, Pictureville, Stockport Plaza and Tideswell.
package venues

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type rule struct {
	re  *regexp.Regexp
	tag string
}

// svRules maps keywords to canonical format tags. Applied to flag labels, notes, titles,
// type/genre strings (Savoy / Plaza Skipton / Penistone).
var svRules = []rule{
	{regexp.MustCompile(`\b3d\b`), "3D"},
	{regexp.MustCompile(`\b35\s?mm\b`), "35mm"},
	{regexp.MustCompile(`\b70\s?mm\b`), "70mm"},
	{regexp.MustCompile(`subtitl|captioned|\bhoh\b`), "Subtitled"},
	{regexp.MustCompile(`audio[ -]?descri`), "Audio Described"},
	{regexp.MustCompile(`relaxed|autism|sensory|dementia`), "Relaxed"},
	{regexp.MustCompile(`parent\s*(&|and)\s*bab|carers?\s*(&|and)\s*bab`), "Parent & Baby"},
	{regexp.MustCompile(`\bq\s*&\s*a\b|\bq and a\b`), "Q&A"},
	{regexp.MustCompile(`live arts|event cinema|\bnt ?live\b|\bntl\b|national theatre live|met opera|royal opera|` +
		`royal ballet|\brbo\b|exhibition on screen`), "Live/Event Cinema"},
}

// svFormats tags the (HTML-escaped) texts, in rule order.
func svFormats(texts ...string) []string {
	parts := make([]string, len(texts))
	for i, t := range texts {
		parts[i] = html.UnescapeString(t)
	}
	blob := strings.ToLower(strings.Join(parts, " "))
	tags := []string{}
	for _, r := range svRules {
		if r.re.MatchString(blob) {
			tags = append(tags, r.tag)
		}
	}
	return tags
}

// fmtRules is the tag vocabulary for Pictureville / Stockport Plaza.
var fmtRules = []rule{
	{regexp.MustCompile(`\bimax\b`), "IMAX"}, {regexp.MustCompile(`\b3d\b`), "3D"},
	{regexp.MustCompile(`\b35\s?mm\b`), "35mm"}, {regexp.MustCompile(`\b70\s?mm\b`), "70mm"},
	{regexp.MustCompile(`subtitl|\bsubs\b|\bhoh\b`), "Subtitled"}, {regexp.MustCompile(`audio[- ]descri`), "Audio Described"},
	{regexp.MustCompile(`relaxed|autism|sensory`), "Relaxed"},
	{regexp.MustCompile(`\bbab(y|ies)\b|parent\s*(&|and)\s*baby`), "Parent & Baby"},
	{regexp.MustCompile(`q\s?&\s?a|\bq and a\b|in conversation|intro(duced|duction) by`), "Q&A"},
	{regexp.MustCompile(`event cinema|\blive\s*:|\blive (?:from|broadcast|with)|\(live\)|encore|nt live|` +
		`national theatre|royal opera|royal ballet|met opera`), "Live/Event Cinema"},
}

func fmtTags(text string) []string {
	t := strings.ToLower(text)
	tags := []string{}
	for _, r := range fmtRules {
		if r.re.MatchString(t) {
			tags = append(tags, r.tag)
		}
	}
	return tags
}

var parentBabyRe = regexp.MustCompile(`parent\s*(&|and)\s*baby`)

// titleTags is fmtTags for a film title. A bare "baby"/"babies" in a title ("Rosemary's Baby", "The Boss
// Baby") is not a parent-and-baby screening, so that tag needs the full phrase; the other rules stay,
// since titles carry genuine event/format words ("RBO ...: The Royal Ballet", "(Subtitled)").
func titleTags(title string) []string {
	tags := fmtTags(title)
	if !parentBabyRe.MatchString(strings.ToLower(title)) {
		tags = slices.DeleteFunc(tags, func(t string) bool { return t == "Parent & Baby" })
	}
	return tags
}

// date builds a calendar date (midnight UTC), rejecting overflow like 29 Feb 2027.
func date(y int, m time.Month, d int) (time.Time, bool) {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return t, t.Month() == m && t.Day() == d
}

// dateCands lists the dates for a yearless day/month from ref's previous year to two years
// on, keeping only those no more than 60 days before ref.
func dateCands(month time.Month, day int, ref time.Time) []time.Time {
	var out []time.Time
	floor := ref.AddDate(0, 0, -60)
	for y := ref.Year() - 1; y <= ref.Year()+2; y++ {
		if d, ok := date(y, month, day); ok && !d.Before(floor) {
			out = append(out, d)
		}
	}
	return out
}

// nearestYear dates a yearless day/month: the earliest occurrence no more than 60 days before ref.
func nearestYear(month time.Month, day int, ref time.Time) (time.Time, error) {
	if c := dateCands(month, day, ref); len(c) > 0 {
		return c[0], nil
	}
	return time.Time{}, fmt.Errorf("bad date %d/%d", day, month)
}

// guessYear dates a yearless UK listing date, preferring the candidate on the given weekday
// (lower-case English name, "" = any), else the nearest one.
func guessYear(day int, month time.Month, wday string, ref time.Time) (time.Time, error) {
	cands := dateCands(month, day, ref)
	if len(cands) == 0 {
		return time.Time{}, fmt.Errorf("bad date %d/%d", day, month)
	}
	if wday != "" {
		for _, d := range cands {
			if strings.ToLower(d.Weekday().String()) == wday {
				return d, nil
			}
		}
	}
	return cands[0], nil
}

// monthAbbr is strptime(s, "%b").month.
func monthAbbr(s string) (time.Month, error) {
	t, err := time.Parse("Jan", s)
	if err != nil {
		return 0, fmt.Errorf("bad month %q", s)
	}
	return t.Month(), nil
}

// inRange reports from <= d <= to for calendar dates.
func inRange(d, from, to time.Time) bool { return !d.Before(from) && !d.After(to) }

// urljoin resolves ref against base like Python's urllib.parse.urljoin.
func urljoin(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// classes is BeautifulSoup's tag.get("class", []) (whitespace-split class list).
func classes(sel *goquery.Selection) []string {
	c, _ := sel.Attr("class")
	return strings.Fields(c)
}

var labelDateRe = regexp.MustCompile(`(?i)\b(?:([a-z]{3})[a-z]*\.?,?\s+)?(\d{1,2})\s*(?:st|nd|rd|th)?\s+([a-z]{3})`)

// labelDate dates a yearless "Sat 31st Oct" / "Sunday, 1st November" / "Sun 01 Nov" label like guessYear
// (the weekday, when given, picks the year). ok is false when the label is not a date.
func labelDate(label string, ref time.Time) (day time.Time, ok bool, err error) {
	m := labelDateRe.FindStringSubmatch(label)
	if m == nil {
		return time.Time{}, false, nil
	}
	mon, err := monthAbbr(strings.ToUpper(m[3][:1]) + strings.ToLower(m[3][1:]))
	if err != nil {
		return time.Time{}, false, nil // e.g. "Tomorrow 5 pm" is not a date
	}
	var wday string
	for d := time.Sunday; d <= time.Saturday; d++ {
		if m[1] != "" && strings.HasPrefix(strings.ToLower(d.String()), strings.ToLower(m[1])) {
			wday = strings.ToLower(d.String())
		}
	}
	n, _ := strconv.Atoi(m[2])
	day, err = guessYear(n, mon, wday, ref)
	return day, err == nil, err
}
