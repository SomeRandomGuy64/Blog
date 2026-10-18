// Package listings turns per-cinema raw shows into the listings.json the page consumes.
package listings

import (
	"html"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

// AdMinutes is the ads/trailers allowance before the film, for end-time estimates.
var AdMinutes = map[string]int{"Odeon": 25, "Vue": 25, "Cineworld": 25, "Everyman": 25, "Showcase": 25,
	"The Light": 25, "Reel": 25, "Picturehouse": 25, "Curzon": 25, "Independent": 10}

const (
	DefaultRuntime = 120            // when no cinema reports a runtime for the film
	StaleAfter     = 12 * time.Hour // a cinema's data is stale when its last success is older
	placeholderRT  = 120            // Picturehouse lists e.g. Digger (129 min) as 120: outvote it
)

// Listings is the listings.json document.
type Listings struct {
	Generated string            `json:"generated"`
	From      string            `json:"from"`
	To        string            `json:"to"`
	Cinemas   []Cinema          `json:"cinemas"`
	Films     map[string]string `json:"films"`
	Shows     []Show            `json:"shows"`
	Premium   []string          `json:"premium"`
	Travel    Travel            `json:"travel"`
	// Set by the server: every region with its cinema count, and the ones this document covers.
	Regions []RegionCount `json:"regions,omitempty"`
	Region  []string      `json:"region,omitempty"`
}

// RegionCount is one entry of Listings.Regions.
type RegionCount struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Cinemas int    `json:"cinemas"`
}

// Cinema is one cinema's header row; Updated/Stale describe the data's freshness.
type Cinema struct {
	Name    string  `json:"name"`
	Chain   string  `json:"chain"`
	URL     string  `json:"url"`
	Error   *string `json:"error"`
	Count   int     `json:"count"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Region  string  `json:"region"`
	Updated *string `json:"updated"`
	Stale   bool    `json:"stale"`
}

// Show is one showing: k film key, c cinema index, t start, s screen, x formats, u booking URL,
// e film end, d runtime, es = e is an estimate.
type Show struct {
	K  string   `json:"k"`
	C  int      `json:"c"`
	T  string   `json:"t"`
	S  *string  `json:"s"`
	X  []string `json:"x"`
	U  *string  `json:"u"`
	Es bool     `json:"es,omitempty"`
	E  string   `json:"e"`
	D  int      `json:"d"`
}

// Travel is the straight-line minutes matrix between cinemas. Unlike the Python output it has
// no Home row/column (and there is no top-level "home"): the API is public, so the page computes
// home legs itself from the visitor's postcode with Estimate.
type Travel struct {
	Source   string         `json:"source"`
	Computed string         `json:"computed"`
	Places   []string       `json:"places"`
	Mins     [][]int        `json:"mins"`
	Coords   [][2]float64   `json:"coords"`
	Estimate EstimateParams `json:"estimate"`
}

// Entry is what Build knows about one cinema: its (last good) shows, the latest error, and
// when it last scraped successfully (zero = never).
type Entry struct {
	Shows   []scrape.Show
	Error   string
	Updated time.Time
}

// FromResults turns fresh scrape results into entries (a success is "updated" when fetched).
func FromResults(rs []scrape.Result) map[string]Entry {
	m := make(map[string]Entry, len(rs))
	for _, r := range rs {
		e := Entry{Shows: r.Shows, Error: string(r.Error)}
		if r.Error == "" {
			e.Updated = r.Fetched
		}
		m[r.Name] = e
	}
	return m
}

var titleFmt = []struct {
	re    *regexp.Regexp
	label string
}{
	{regexp.MustCompile(`(?i)\bimax\b`), "IMAX"}, {regexp.MustCompile(`(?i)\b3d\b`), "3D"},
	{regexp.MustCompile(`(?i)\b4dx\b`), "4DX"}, {regexp.MustCompile(`(?i)\bscreenx\b`), "ScreenX"},
	{regexp.MustCompile(`(?i)\b(subtitled|subbed)\b`), "Subtitled"}, {regexp.MustCompile(`(?i)\bdubbed\b`), "Dubbed"},
	{regexp.MustCompile(`(?i)\b70 ?mm\b`), "70mm"}, {regexp.MustCompile(`(?i)\b35 ?mm\b`), "35mm"},
}

var (
	titleBracket = regexp.MustCompile(`(?i)\s*[\(\[](?:2D|3D|IMAX|4DX|ScreenX|subtitled|subbed|dubbed|U|PG|12A?|15|18|TBC|\d{4})[\)\]]`)
	titleSuffix  = regexp.MustCompile(`(?i)\s*[-:–|]\s*(?:(?:the|in|imax|3d|4dx|screenx|70 ?mm|35 ?mm|experience|print)\b\s*)+$`)
	titleMM      = regexp.MustCompile(`(?i)\s*[\(\[](?:in )?(?:imax |)(?:70|35) ?mm[\)\]]`)
	titleTail    = regexp.MustCompile(`\s+(?:in )?(?:IMAX|3D|4DX|ScreenX|70mm|70MM|35mm|35MM)$`)
	keyThe       = regexp.MustCompile(`^the\s+`)
	keyJunk      = regexp.MustCompile(`[^a-z0-9]`)
)

// CleanTitle pulls format words out of titles ("Spirited Away (Dubbed)") so the same film
// groups across cinemas, and returns the extra tags they implied.
func CleanTitle(t string) (string, []string) {
	var extra []string
	for _, f := range titleFmt {
		if f.re.MatchString(t) {
			extra = append(extra, f.label)
		}
	}
	t = titleBracket.ReplaceAllString(t, "")
	t = titleSuffix.ReplaceAllString(t, "")
	t = titleMM.ReplaceAllString(t, "")
	t = titleTail.ReplaceAllString(t, "")
	return scrape.Squash(t), extra
}

// FilmKey is the grouping key for a cleaned title.
func FilmKey(t string) string {
	t = strings.ReplaceAll(strings.ToLower(t), "&", "and")
	t = keyThe.ReplaceAllString(t, "")
	return keyJunk.ReplaceAllString(t, "")
}

type pending struct {
	Show
	cinema string
	end    string
	dur    int
}

type dedupeKey struct{ k, t, s, x string }

// Build assembles listings for from..to (inclusive) from per-cinema entries; cinemas without
// an entry are reported as not fetched yet.
func Build(cinemas []scrape.Cinema, entries map[string]Entry, from, to, now time.Time) *Listings {
	cin := make([]Cinema, len(cinemas))
	var shows []*pending
	names := map[string]map[string]int{} // key -> title -> votes
	nameOrder := map[string][]string{}   // first-seen order breaks ties, like Counter.most_common
	runtimes := map[string]map[string]int{}
	for i, c := range cinemas {
		e, ok := entries[c.Name]
		cin[i] = Cinema{Name: c.Name, Chain: c.Chain, URL: c.URL, Region: c.Region}
		switch {
		case !ok:
			msg := "not fetched yet"
			cin[i].Error = &msg
		case e.Error != "":
			msg := e.Error
			cin[i].Error = &msg
		}
		if !e.Updated.IsZero() {
			u := e.Updated.In(scrape.London).Format(time.RFC3339)
			cin[i].Updated = &u
		}
		cin[i].Stale = e.Updated.IsZero() || now.Sub(e.Updated) > StaleAfter
		seen := map[dedupeKey]bool{}
		for _, x := range e.Shows {
			if !scrape.InWindow(x.Start, from, to) {
				continue
			}
			if _, err := time.Parse(scrape.TimeFmt, x.Start); err != nil {
				continue
			}
			// Some sources (The Light) leave HTML entities in titles: "We&#39;re Going on a Bear Hunt".
			title, extra := CleanTitle(html.UnescapeString(x.Film))
			fmts := scrape.Tags(append(slices.Clone(x.Formats), extra...), false)
			k := FilmKey(title)
			dk := dedupeKey{k, x.Start, x.Screen, strings.Join(fmts, "\x00")}
			if seen[dk] {
				continue
			}
			seen[dk] = true
			if names[k] == nil {
				names[k] = map[string]int{}
			}
			if names[k][title] == 0 {
				nameOrder[k] = append(nameOrder[k], title)
			}
			names[k][title]++
			cin[i].Count++
			if x.Duration != 0 {
				if runtimes[k] == nil {
					runtimes[k] = map[string]int{}
				}
				if _, ok := runtimes[k][c.Name]; !ok { // one vote per cinema
					runtimes[k][c.Name] = x.Duration
				}
			}
			shows = append(shows, &pending{Show: Show{K: k, C: i, T: x.Start, S: optStr(x.Screen), X: fmts, U: optStr(x.URL)},
				cinema: c.Name, end: x.End, dur: x.Duration})
		}
	}

	// "Classic" only marks a (cinema, film) whose every showing is a classic re-release.
	type cf struct {
		c int
		k string
	}
	groups := map[cf][]*pending{}
	for _, sh := range shows {
		groups[cf{sh.C, sh.K}] = append(groups[cf{sh.C, sh.K}], sh)
	}
	for _, g := range groups {
		if slices.ContainsFunc(g, func(sh *pending) bool { return !slices.Contains(sh.X, "Classic") }) {
			for _, sh := range g {
				sh.X = slices.DeleteFunc(sh.X, func(f string) bool { return f == "Classic" })
			}
		}
	}

	med := map[string]int{}
	for k, v := range runtimes {
		var all, real []float64
		for _, d := range v {
			all = append(all, float64(d))
			if d != placeholderRT {
				real = append(real, float64(d))
			}
		}
		if len(real) == 0 {
			real = all
		}
		med[k] = int(math.RoundToEven(median(real)))
	}

	out := make([]Show, 0, len(shows))
	for _, sh := range shows {
		dur := sh.dur
		if dur == placeholderRT || dur == 0 {
			dur = med[sh.K]
		}
		t0, _ := time.Parse(scrape.TimeFmt, sh.T)
		ads, ok := AdMinutes[cin[sh.C].Chain]
		if !ok {
			ads = 10
		}
		fin, span := sh.end, 0
		var tf time.Time
		if fin != "" {
			var err error
			if tf, err = parseISO(fin); err != nil {
				fin = ""
			} else {
				span = floorDiv(int(tf.Sub(t0).Seconds()), 60)
			}
		}
		if fin != "" && span >= max(dur, 1) { // max(dur, 1) == Python's (dur or 1)
			if dur == 0 {
				dur = max(1, span-ads)
			}
		} else { // no end, or a slot shorter than the film (Showroom's Digger) is bogus
			if dur == 0 {
				dur = DefaultRuntime
			}
			fin, sh.Es = t0.Add(time.Duration(ads+dur)*time.Minute).Format(scrape.TimeFmt), true
		}
		sh.E, sh.D = fin, dur
		out = append(out, sh.Show)
	}

	films := make(map[string]string, len(names))
	for k, votes := range names {
		best := ""
		for _, t := range nameOrder[k] {
			if best == "" || votes[t] > votes[best] {
				best = t
			}
		}
		films[k] = best
	}

	tr := travelMatrix(cinemas, now)
	for i := range cin {
		cin[i].Lat, cin[i].Lng = tr.Coords[i][0], tr.Coords[i][1]
	}
	premium := slices.Clone(scrape.Premium)
	slices.Sort(premium)
	return &Listings{
		Generated: now.In(scrape.London).Format(time.RFC3339),
		From:      from.Format(scrape.DateFmt), To: to.Format(scrape.DateFmt),
		Cinemas: cin, Films: films, Shows: out, Premium: premium, Travel: tr,
	}
}

// travelMatrix is the straight-line estimate matrix; the page fetches Google times itself
// when the user supplies their own key.
func travelMatrix(cinemas []scrape.Cinema, now time.Time) Travel {
	n := len(cinemas)
	t := Travel{Source: "estimate", Computed: now.In(scrape.London).Format(scrape.DateFmt),
		Estimate: Estimate, Places: make([]string, 0, n), Coords: make([][2]float64, 0, n)}
	for _, c := range cinemas {
		t.Places = append(t.Places, c.Name)
		t.Coords = append(t.Coords, [2]float64{c.Lat, c.Lng}) // registry_test guarantees every cinema is located
	}
	t.Mins = make([][]int, n)
	for i := range n {
		t.Mins[i] = make([]int, n)
		for j := range n {
			if i != j {
				a, b := t.Coords[i], t.Coords[j]
				t.Mins[i][j] = EstimateMinutes(HaversineKm(a[0], a[1], b[0], b[1]))
			}
		}
	}
	return t
}

func median(xs []float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// parseISO reads "2006-01-02T15:04" (already truncated by the scraper) or longer ISO forms.
func parseISO(s string) (time.Time, error) {
	if len(s) > 16 {
		s = s[:16]
	}
	return time.Parse(scrape.TimeFmt, s)
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
