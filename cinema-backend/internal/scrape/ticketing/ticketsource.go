package ticketing

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

// TicketSource "What's On" pages are Vue apps backed by a public Meilisearch index
// (key/host/index embedded in every whats-on page's #app-config JSON). One hit = one
// performance. `timestamp` is UK *wall-clock* time encoded as if it were UTC (verified
// against dateTimeString), so decode it as UTC.

type tsConfig struct{ host, index, key string }

var tsFallback = tsConfig{"https://search.ticketsource.com", "events_PROD",
	"8a9086965b57cfe51cf3bcdf05f9380b4673cae831135a2eb48afcbbac9d991b"}

// tsCfg reads the search config once per run, falling back to the known one on any error.
func tsCfg(s *scrape.Session) tsConfig {
	v, _ := s.Memo("ts-cfg", func() (any, error) {
		r, err := s.Get("https://www.ticketsource.com/whats-on/glossop/partington-players-theatre")
		if err != nil {
			return tsFallback, nil
		}
		doc, err := r.Doc()
		if err != nil {
			return tsFallback, nil
		}
		var c struct {
			Host  string `json:"meiliHostUrl"`
			Index string `json:"meiliIndex"`
			Key   string `json:"meiliApiKey"`
		}
		if json.Unmarshal([]byte(doc.Find("script#app-config").First().Text()), &c) != nil ||
			c.Host == "" || c.Index == "" || c.Key == "" {
			return tsFallback, nil
		}
		return tsConfig{c.Host, c.Index, c.Key}, nil
	})
	return v.(tsConfig)
}

type tsHit struct {
	Event    string `json:"event"`
	Hint     any    `json:"hint"`
	Genre    any    `json:"genre"`
	Category string `json:"category"`
	Stamp    any    `json:"timestamp"`
	Promoter string `json:"promoterNameURL"`
	Slug     string `json:"eventSlug"`
	TimeHash string `json:"timeHash"`

	at  time.Time // naive UK wall-clock (held as UTC)
	url string
}

// tsSearch returns all TicketSource performances at venueSlugs on from..to (inclusive).
func tsSearch(s *scrape.Session, venueSlugs []string, from, to time.Time, categories ...string) ([]tsHit, error) {
	cfg := tsCfg(s)
	quote := func(xs []string) string {
		q := make([]string, len(xs))
		for i, x := range xs {
			b, _ := json.Marshal(x)
			q[i] = string(b)
		}
		return strings.Join(q, ", ")
	}
	filt := fmt.Sprintf("venueSlug IN [%s] AND timestamp >= %d AND timestamp < %d",
		quote(venueSlugs), from.Unix(), to.Unix()+86400)
	if len(categories) > 0 {
		filt += fmt.Sprintf(" AND category IN [%s]", quote(categories))
	}
	var hits []tsHit
	for off := 0; ; off += 500 {
		var page struct{ Hits []tsHit }
		err := s.PostJSON(cfg.host+"/indexes/"+cfg.index+"/search", &page,
			scrape.Header("Authorization", "Bearer "+cfg.key),
			scrape.JSONBody(map[string]any{"q": "", "filter": filt, "sort": []string{"timestamp:asc"},
				"limit": 500, "offset": off}))
		if err != nil {
			return nil, err
		}
		hits = append(hits, page.Hits...)
		if len(page.Hits) < 500 {
			break
		}
	}
	for i := range hits {
		h := &hits[i]
		h.at = time.Unix(int64(scrape.ToInt(h.Stamp)), 0).UTC()
		h.url = fmt.Sprintf("https://www.ticketsource.com/%s/%s/%s/%s/%s",
			h.Promoter, h.Slug, h.at.Format(scrape.DateFmt), h.at.Format("15:04"), h.TimeHash)
	}
	return hits, nil
}

type rule struct {
	re  *regexp.Regexp
	tag string
}

var fmtRules = []rule{
	{regexp.MustCompile(`\bsubtitle|\[subtitled\]|\bsubbed\b`), "Subtitled"},
	{regexp.MustCompile(`\bnt live\b|national theatre live|\brbo\b|royal ballet|royal opera|met opera|\blive-performance\b`),
		"Live/Event Cinema"},
	{regexp.MustCompile(`\brelaxed\b|autism.friendly|sensory.friendly`), "Relaxed"},
	{regexp.MustCompile(`parent (?:&|and) baby|parent.and.toddler|carers? (?:&|and) babies`), "Parent & Baby"},
	{regexp.MustCompile(`\bq ?& ?a\b|special guest|\bintro(?:duction|duced)? by\b`), "Q&A"},
	{regexp.MustCompile(`\bsilent\b`), "Silent"},
	{regexp.MustCompile(`live (?:accompaniment|score|music)`), "Live Music"},
	{regexp.MustCompile(`\b3d\b`), "3D"},
	{regexp.MustCompile(`\b35 ?mm\b`), "35mm"},
	{regexp.MustCompile(`\b70 ?mm\b`), "70mm"},
	{regexp.MustCompile(`double bill`), "Double Bill"},
	{regexp.MustCompile(`dementia`), "Dementia Friendly"},
	{regexp.MustCompile(`sing.?a.?long`), "Sing-along"},
}

// formats tags free text (title, hint, genre...) with fmtRules.
func formats(texts ...string) []string {
	t := strings.ToLower(strings.Join(texts, " "))
	out := []string{}
	for _, r := range fmtRules {
		if r.re.MatchString(t) {
			out = append(out, r.tag)
		}
	}
	return scrape.Uniq(out)
}

var (
	small = map[string]bool{"a": true, "an": true, "the": true, "of": true, "and": true, "in": true, "on": true,
		"at": true, "to": true, "for": true, "by": true, "with": true, "from": true, "or": true, "vs": true,
		"de": true, "la": true, "le": true, "du": true, "des": true}
	romanRe = regexp.MustCompile(`^[ivxlc]+[:,]?$`)
)

// titlecase title-cases ALL-CAPS strings (mixed case is left alone).
func titlecase(t string) string {
	if t != strings.ToUpper(t) {
		return t
	}
	words := strings.Split(t, " ")
	out := make([]string, 0, len(words))
	for i, w := range words {
		lw := strings.ToLower(w)
		switch {
		case romanRe.MatchString(lw) && len(strings.Trim(lw, ":,")) > 1:
			out = append(out, w) // roman numerals
		case i > 0 && small[lw] && !strings.HasSuffix(out[len(out)-1], ":"):
			out = append(out, lw)
		default:
			parts := strings.Split(lw, "-")
			for j, p := range parts {
				parts[j] = upperFirst(p)
			}
			out = append(out, strings.Join(parts, "-"))
		}
	}
	return strings.Join(out, " ")
}

func tsRows(hits []tsHit, clean func(string) string, extra func(tsHit) []string) []scrape.Show {
	rows := make([]scrape.Show, 0, len(hits))
	for _, h := range hits {
		hint, _ := h.Hint.(string)
		genre, _ := h.Genre.(string)
		fm := formats(h.Event, hint, genre)
		if extra != nil {
			fm = append(fm, extra(h)...)
		}
		rows = append(rows, scrape.Show{Film: clean(h.Event), Start: h.at.Format(scrape.TimeFmt),
			Formats: scrape.Uniq(fm), URL: h.url})
	}
	return rows
}

var yearRe = regexp.MustCompile(`\s*\((?:19|20)\d\d\)\s*$`)

func stripYear(t string) string { return strings.TrimSpace(yearRe.ReplaceAllString(t, "")) }

// StockportGarrick: only TicketSource events categorised as film.
func StockportGarrick(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	hits, err := tsSearch(s, []string{"stockport-garrick-theatre"}, from, to, "film")
	if err != nil {
		return nil, err
	}
	return tsRows(hits, func(t string) string { return titlecase(strings.TrimSpace(t)) }, nil), nil
}

var (
	leighPrefix = regexp.MustCompile(`(?i)^(?:Fear at the Factory|Midweek Matinee|Stitch (?:&|&amp;) Screen|Family Film|` +
		`Leigh Film Society presents\.*)\s*[-:–]?\s*`)
	leighSuffix = regexp.MustCompile(`(?i)\s*(?:-\s*Pay As You Feel|National Theatre Live at Leigh Film Factory|` +
		`with very Special Guest|with live accompaniment.*|\d+(?:st|nd|rd|th) Anniversary)\s*$`)
)

func leighTitle(t string) string {
	t = leighPrefix.ReplaceAllString(strings.TrimSpace(t), "")
	return stripYear(leighSuffix.ReplaceAllString(t, ""))
}

// LeighFilmFactory: a cinema, so its 'theatre' category = NT Live/filmed stage.
func LeighFilmFactory(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	hits, err := tsSearch(s, []string{"leigh-film-factory"}, from, to, "film", "theatre")
	if err != nil {
		return nil, err
	}
	return tsRows(hits, leighTitle, func(h tsHit) []string {
		if h.Category == "theatre" {
			return []string{"Live/Event Cinema"}
		}
		return nil
	}), nil
}

var (
	bracketRe  = regexp.MustCompile(`\[[^\]]*\]`)
	bijouCert  = regexp.MustCompile(`(?i)\((?:U|PG|12A?|15|18|TBC|CTBA|R18)?(?:,\s*)?(?:\d{4})?\)`)
	bijouNT    = regexp.MustCompile(`(?i)^NT LIVE\s*[-:–]\s*`)
	spaceRunRe = regexp.MustCompile(`\s+`)
)

// bijouTitle cleans titles like 'AMÉLIE (15, 2001) [SUBTITLED]'.
func bijouTitle(t string) string {
	t = bracketRe.ReplaceAllString(t, "")
	t = bijouCert.ReplaceAllString(t, "")
	t = bijouNT.ReplaceAllString(strings.TrimSpace(t), "")
	return titlecase(strings.Trim(spaceRunRe.ReplaceAllString(t, " "), " -"))
}

// SouthportBijou: titles look like 'AMÉLIE (15, 2001) [SUBTITLED]'.
func SouthportBijou(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	hits, err := tsSearch(s, []string{"southport-bijou-cinema"}, from, to, "film")
	if err != nil {
		return nil, err
	}
	return tsRows(hits, bijouTitle, nil), nil
}

var (
	peakDateRe  = regexp.MustCompile(`^\d{1,2} \w+ 20\d\d`)
	altTitleRe  = regexp.MustCompile(`\s*[\[(][^\])]*[\])]\s*$`)
	partingtons = []string{"partington-players-theatre", "partington-theatre"}
)

// peakSubtitled returns the lower-cased titles the Peak Film Society listing marks as subtitled
// (each listing is a "7 October 2026" line, the title, then details).
func peakSubtitled(body []byte) map[string]bool {
	subbed := map[string]bool{}
	doc, err := scrape.Doc(body)
	if err != nil {
		return subbed
	}
	lines := strings.Split(scrape.Text(doc.Selection, "\n", true), "\n")
	title := ""
	for i, ln := range lines {
		if peakDateRe.MatchString(ln) && i+1 < len(lines) {
			title = strings.ToLower(strings.TrimSpace(lines[i+1]))
		} else if title != "" && strings.Contains(strings.ToLower(ln), "subtitles") {
			subbed[title] = true
		}
	}
	return subbed
}

// PartingtonGlossop: Peak Film Society screenings at the Partington Theatre (TicketSource).
func PartingtonGlossop(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	hits, err := tsSearch(s, partingtons, from, to, "film")
	if err != nil {
		return nil, err
	}
	subbed := map[string]bool{}
	if len(hits) > 0 { // TicketSource descriptions are truncated; subtitle info lives on the society site
		if r, err := s.GetOK("https://peakfilmsociety.org/whats-on/"); err == nil {
			subbed = peakSubtitled(r.Body)
		}
	}
	// drop a trailing [original title]
	alt := func(t string) string { return altTitleRe.ReplaceAllString(strings.TrimSpace(t), "") }
	return tsRows(hits, alt, func(h tsHit) []string {
		if subbed[strings.ToLower(strings.TrimSpace(h.Event))] {
			return []string{"Subtitled"}
		}
		return nil
	}), nil
}

var (
	mandsYears = regexp.MustCompile(`\d{1,2} \w{3} (20\d\d)\s*[-–]\s*\d{1,2} \w{3} (20\d\d)`)
	mandsItem  = regexp.MustCompile(`(?i)^\s*(\d{1,2})(?:st|nd|rd|th)?\s+([a-z]+)\s*//`)
	mandsCert  = regexp.MustCompile(`(?i)\s*\(CERT\.?[^)]*\)\s*$`)
	parenEnd   = regexp.MustCompile(`\s*\([^)]*\)\s*$`)
	months     = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
)

// mandsFilmSociety: Manchester & Salford Film Society (members/guests; pay at door) - all shows
// 7:30pm at Altrincham Little Theatre. Listing items read e.g. '17th october // the maid'.
func mandsFilmSociety(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	r, err := s.GetOK("https://mandsfilmsociety.org.uk/")
	if err != nil {
		return nil, err
	}
	doc, err := r.Doc()
	if err != nil {
		return nil, err
	}
	y1, y2 := from.Year(), from.Year()+1
	if m := mandsYears.FindStringSubmatch(scrape.Text(doc.Selection, " ", false)); m != nil {
		fmt.Sscan(m[1], &y1)
		fmt.Sscan(m[2], &y2)
	}
	var rows []scrape.Show
	for _, art := range doc.Find("article.elementor-post").EachIter() {
		a := art.Find(".elementor-post__title a").First()
		if a.Length() == 0 {
			continue
		}
		m := mandsItem.FindStringSubmatch(scrape.Text(a, " ", false))
		if m == nil {
			continue
		}
		mon, ok := months[strings.ToLower(headRunes(m[2], 3))]
		if !ok {
			continue // Python's strptime would raise and lose the whole listing
		}
		var day int
		fmt.Sscan(m[1], &day)
		y := y2
		if mon >= 7 {
			y = y1
		}
		d := time.Date(y, mon, day, 0, 0, 0, 0, time.UTC)
		if d.Day() != day || d.Before(from) || d.After(to) {
			continue
		}
		ex := art.Find(".elementor-post__excerpt").First()
		var first, info string
		if ex.Length() > 0 {
			first, _, _ = strings.Cut(scrape.Text(ex, "\n", true), "\n")
			info = scrape.Text(ex, " ", false)
		} else {
			_, first, _ = strings.Cut(scrape.Text(a, "", false), "//")
		}
		title := mandsCert.ReplaceAllString(strings.TrimSpace(first), "")
		title = parenEnd.ReplaceAllString(title, "") // drop '(La nana)' original title
		rows = append(rows, scrape.Show{Film: titlecase(title), Start: d.Format(scrape.DateFmt) + "T19:30",
			Formats: formats(info), URL: scrape.Attr(a, "href")})
	}
	return rows, nil
}

var littleClean = regexp.MustCompile(`(?i)\s+(?:\d{4} )?a film by .*$|\s+film \w+ in our .*$`)

// AltrinchamLittleTheatre: film-category TicketSource events at the venue (e.g. Manchester Film
// Weekender's French film season) + Manchester & Salford Film Society's season. Either source
// may fail on its own.
func AltrinchamLittleTheatre(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	var rows []scrape.Show
	hits, err1 := tsSearch(s, []string{"altrincham-little-theatre"}, from, to, "film")
	if err1 == nil {
		rows = tsRows(hits, func(t string) string { return littleClean.ReplaceAllString(strings.TrimSpace(t), "") }, nil)
	}
	mands, err2 := mandsFilmSociety(s, from, to)
	rows = append(rows, mands...)
	if err1 != nil && err2 != nil {
		return nil, errors.Join(err1, err2)
	}
	if (err1 != nil || err2 != nil) && len(rows) == 0 {
		return nil, errors.Join(err1, err2)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Start < rows[j].Start })
	return rows, nil
}
