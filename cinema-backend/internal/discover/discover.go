// Package discover finds every UK cinema of the big chains from the chains' own sites and APIs
// and merges them into internal/registry/chains.json (`cinema discover`).
package discover

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/region"
	"fazon.nexus/cinema/internal/scrape"
)

// Chains is every chain discovery covers, with its finder.
var Chains = []struct {
	Name string
	find finder
}{
	{"Odeon", odeon}, {"Curzon", curzon},
	{"Cineworld", webedia("Cineworld", "https://www.cineworld.co.uk", "/page-data/cinemas/page-data.json")},
	{"Everyman", webedia("Everyman", "https://www.everymancinema.com", "/page-data/venues-list/page-data.json")},
	{"Showcase", webedia("Showcase", "https://www.showcasecinemas.co.uk", "/page-data/cinemas/page-data.json")},
	{"Vue", vue}, {"Picturehouse", picturehouse}, {"The Light", light}, {"Reel", reel},
}

// finder lists one chain's cinemas. Entries may lack coordinates (geocoded from Postcode) and
// Region (always filled from postcodes.io); log gets per-cinema notes.
type finder func(s *scrape.Session, log func(string, ...any)) ([]chainfile.Entry, error)

// perHost caps parallel requests to one site.
const perHost = 4

// Options configures Run.
type Options struct {
	Only []string  // chain names, case-insensitive substrings; empty = all
	Log  io.Writer // warnings and progress
}

// Summary is one chain's outcome.
type Summary struct {
	Chain   string
	Count   int
	Added   []string
	Removed []string
	Skipped []string // found but missing name/coords/region
	Kept    string   // why the previous entries were kept instead ("" = discovery used)
}

// Run discovers the selected chains and merges them into prev (chains not selected keep their
// previous entries). The result is sorted by Chain, then Name; names are checked for uniqueness.
func Run(ctx context.Context, prev chainfile.File, opt Options) (chainfile.File, []Summary, error) {
	logw := opt.Log
	if logw == nil {
		logw = io.Discard
	}
	var mu sync.Mutex
	logf := func(format string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(logw, format+"\n", a...)
	}
	byChain := map[string][]chainfile.Entry{}
	for _, e := range prev.Cinemas {
		byChain[e.Chain] = append(byChain[e.Chain], e)
	}
	var (
		wg   sync.WaitGroup
		sums = make([]*Summary, len(Chains))
		outs = make([][]chainfile.Entry, len(Chains))
	)
	for i, c := range Chains {
		if !selected(c.Name, opt.Only) {
			outs[i] = byChain[c.Name]
			continue
		}
		wg.Go(func() {
			start := time.Now()
			cur, err := findChain(ctx, c.Name, c.find, logf)
			out, sum := mergeChain(c.Name, byChain[c.Name], cur, err)
			for _, s := range sum.Skipped {
				logf("%s: skipping %s", c.Name, s)
			}
			if sum.Kept != "" {
				logf("WARNING %s: keeping the %d previous entries: %s", c.Name, len(out), sum.Kept)
			}
			logf("%s: done in %.0fs", c.Name, time.Since(start).Seconds())
			outs[i], sums[i] = out, &sum
		})
	}
	wg.Wait()
	var f chainfile.File
	f.Generated = time.Now().In(scrape.London).Format(scrape.DateFmt)
	for _, o := range outs {
		f.Cinemas = append(f.Cinemas, o...)
	}
	slices.SortFunc(f.Cinemas, func(a, b chainfile.Entry) int {
		return cmp.Or(cmp.Compare(a.Chain, b.Chain), cmp.Compare(a.Name, b.Name))
	})
	var summaries []Summary
	for _, s := range sums {
		if s != nil {
			summaries = append(summaries, *s)
		}
	}
	return f, summaries, uniqueNames(f.Cinemas)
}

func selected(chain string, only []string) bool {
	if len(only) == 0 {
		return true
	}
	for _, o := range only {
		if o = strings.ToLower(strings.TrimSpace(o)); o != "" && strings.Contains(strings.ToLower(chain), o) {
			return true
		}
	}
	return false
}

// findChain runs one finder and geolocates its entries (dropping non-UK ones).
func findChain(ctx context.Context, chain string, find finder, logf func(string, ...any)) ([]chainfile.Entry, error) {
	s, err := scrape.NewSession(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	log := func(format string, a ...any) { logf(chain+": "+format, a...) }
	es, err := find(s, log)
	if err != nil {
		return nil, err
	}
	for i := range es {
		es[i].Chain = chain
	}
	return locate(s, es, log)
}

// key identifies an entry across runs: Chain + ID, or Chain + Slug when there is no ID.
func key(e chainfile.Entry) string {
	if e.ID != "" {
		return e.Chain + "\x00id\x00" + e.ID
	}
	return e.Chain + "\x00slug\x00" + e.Slug
}

func complete(e chainfile.Entry) bool {
	return e.Name != "" && e.Lat != 0 && e.Lng != 0 && region.Valid(e.Region)
}

// mergeChain merges one chain's fresh discovery into its previous entries: a matched entry keeps
// its previous name (and borrows its location if the new one lacks it); a failed or shrunken
// (< half) discovery keeps the previous entries wholesale.
func mergeChain(chain string, prev, cur []chainfile.Entry, err error) ([]chainfile.Entry, Summary) {
	sum := Summary{Chain: chain}
	old := map[string]chainfile.Entry{}
	for _, e := range prev {
		old[key(e)] = e
	}
	var out []chainfile.Entry
	seen := map[string]bool{}
	for _, e := range cur {
		k := key(e)
		if seen[k] {
			sum.Skipped = append(sum.Skipped, fmt.Sprintf("%q: duplicate id/slug", e.Name))
			continue
		}
		if p, ok := old[k]; ok {
			e.Name = p.Name
			if e.Lat == 0 || e.Lng == 0 || !region.Valid(e.Region) {
				e.Lat, e.Lng, e.Region = p.Lat, p.Lng, p.Region
				e.Postcode, e.Address = cmp.Or(e.Postcode, p.Postcode), cmp.Or(e.Address, p.Address)
			}
		}
		if !complete(e) {
			sum.Skipped = append(sum.Skipped, fmt.Sprintf("%q (id %q, slug %q): missing name, coordinates or region", e.Name, e.ID, e.Slug))
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	switch {
	case err != nil:
		sum.Kept = err.Error()
	case len(out)*2 < len(prev):
		sum.Kept = fmt.Sprintf("found only %d (previously %d)", len(out), len(prev))
	}
	if sum.Kept != "" {
		sum.Count = len(prev)
		return prev, sum
	}
	for _, e := range out {
		if _, ok := old[key(e)]; !ok {
			sum.Added = append(sum.Added, e.Name)
		}
	}
	for _, e := range prev {
		if !seen[key(e)] {
			sum.Removed = append(sum.Removed, e.Name)
		}
	}
	sum.Count = len(out)
	return out, sum
}

func uniqueNames(es []chainfile.Entry) error {
	by := map[string][]string{}
	for _, e := range es {
		by[e.Name] = append(by[e.Name], e.Chain+" "+cmp.Or(e.ID, e.Slug))
	}
	var dups []string
	for n, ks := range by {
		if len(ks) > 1 {
			dups = append(dups, fmt.Sprintf("%q (%s)", n, strings.Join(ks, ", ")))
		}
	}
	if len(dups) > 0 {
		slices.Sort(dups)
		return fmt.Errorf("duplicate cinema names: %s", strings.Join(dups, "; "))
	}
	return nil
}

// PrintSummary writes one line per chain plus the added/removed/skipped names.
func PrintSummary(w io.Writer, f chainfile.File, sums []Summary) {
	for _, s := range sums {
		line := fmt.Sprintf("%-13s %4d  +%d -%d", s.Chain, s.Count, len(s.Added), len(s.Removed))
		if len(s.Skipped) > 0 {
			line += fmt.Sprintf("  (%d skipped)", len(s.Skipped))
		}
		if s.Kept != "" {
			line += "  KEPT PREVIOUS: " + s.Kept
		}
		fmt.Fprintln(w, line)
		for _, n := range s.Added {
			fmt.Fprintln(w, "    + "+n)
		}
		for _, n := range s.Removed {
			fmt.Fprintln(w, "    - "+n)
		}
		for _, n := range s.Skipped {
			fmt.Fprintln(w, "    ! "+n)
		}
	}
	regions := map[string]int{}
	for _, e := range f.Cinemas {
		regions[e.Region]++
	}
	var parts []string
	for _, r := range region.All {
		if n := regions[r.Key]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", r.Key, n))
		}
	}
	fmt.Fprintf(w, "%d cinemas: %s\n", len(f.Cinemas), strings.Join(parts, ", "))
}

// each runs fn over items with at most n in flight.
func each[T any](n int, items []T, fn func(int, T)) {
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for i, it := range items {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fn(i, it)
		})
	}
	wg.Wait()
}

// get is GetOK with one retry for transient failures.
func get(s *scrape.Session, u string, opts ...scrape.Opt) (*scrape.Resp, error) {
	r, err := s.GetOK(u, opts...)
	if err != nil {
		if he, ok := err.(*scrape.HTTPError); ok && he.Status == 404 {
			return r, err
		}
		select {
		case <-s.Context().Done():
			return r, err
		case <-time.After(2 * time.Second):
		}
		r, err = s.GetOK(u, opts...)
	}
	return r, err
}

var (
	nonSlug  = regexp.MustCompile(`[^a-z0-9]+`)
	postcode = regexp.MustCompile(`\b([A-Z]{1,2}[0-9][A-Z0-9]?) ?([0-9][A-Z]{2})\b`)
	spaces   = regexp.MustCompile(`\s+`)
)

// chainName is "<chain> <place>", tidied ("London - Wembley" → "London Wembley"), without
// doubling a chain prefix the place already has.
func chainName(chain, place string) string {
	place = strings.TrimSpace(spaces.ReplaceAllString(strings.ReplaceAll(place, " - ", " "), " "))
	if len(place) > len(chain) && strings.EqualFold(place[:len(chain)+1], chain+" ") {
		place = place[len(chain)+1:]
	}
	return chain + " " + place
}

// slugify lowercases s and joins its alphanumeric runs with hyphens.
func slugify(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// findPostcode returns the last UK postcode in s, normalised to "OUT IN".
func findPostcode(s string) string {
	ms := postcode.FindAllStringSubmatch(strings.ToUpper(s), -1)
	if len(ms) == 0 {
		return ""
	}
	m := ms[len(ms)-1]
	return m[1] + " " + m[2]
}

// joinAddress joins address lines (themselves possibly comma-separated) with ", ", dropping
// blanks and postcodes.
func joinAddress(lines ...string) string {
	var out []string
	for _, l := range lines {
		for _, p := range strings.Split(l, ",") {
			p = strings.TrimSpace(postcode.ReplaceAllString(spaces.ReplaceAllString(p, " "), ""))
			if _, err := strconv.ParseFloat(p, 64); p == "" || err == nil { // stray coordinates
				continue
			}
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}
