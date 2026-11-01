package listings

import (
	"html"
	"math"
	"slices"
	"time"

	"fazon.nexus/cinema/internal/films"
	"fazon.nexus/cinema/internal/scrape"
)

// runtimeGap is how far two listings' runtimes may differ, in minutes, before the title rules
// stop treating them as one film. Decided merges ignore it, because director's cuts differ.
const runtimeGap = 20

// maxSimilar caps the suggestions sent for one film.
const maxSimilar = 5

// FilmNote is sent for a film that has merged names or possible duplicates. Names maps each
// listing's own key to its title, S lists possibly the same films, and U marks a match nobody
// has confirmed, which the page badges.
type FilmNote struct {
	Names map[string]string `json:"n,omitempty"`
	S     []string          `json:"s,omitempty"`
	U     bool              `json:"u,omitempty"`
}

type rawFilm struct {
	key     string // ordinary film key
	title   string // most common title
	votes   map[string]int
	match   string // films.Key of the base title
	base    string
	durs    map[string]int // cinema -> runtime
	shows   int
	decided bool // merged by the owner, or kept apart
	talk    bool // a Q&A runs on after the film, so its runtime proves nothing
}

func (r *rawFilm) runtime() int { // 0 = unknown
	if r.talk {
		return 0
	}
	var real []float64
	for _, d := range r.durs {
		if d != placeholderRT {
			real = append(real, float64(d))
		}
	}
	if len(real) == 0 {
		return 0
	}
	return int(math.RoundToEven(median(real)))
}

// matchKey is the rules' key for a cleaned title, falling back to its ordinary key.
func matchKey(title string) (key, base string, tags []string) {
	base, tags = films.Base(title)
	if key = films.Key(base); key == "" {
		key = FilmKey(title)
	}
	return key, base, tags
}

// Bases lists the base titles on sale in from..to, by match key, for the film database lookups.
func Bases(entries map[string]Entry, from, to time.Time) map[string]string {
	out := map[string]string{}
	for _, e := range entries {
		for _, x := range e.Shows {
			if !scrape.InWindow(x.Start, from, to) {
				continue
			}
			title, _ := CleanTitle(html.UnescapeString(x.Film))
			if k, b, _ := matchKey(title); k != "" {
				out[k] = b
			}
		}
	}
	return out
}

// scanFilms is Build's first pass: every listing in the window under its ordinary key.
func scanFilms(cinemas []scrape.Cinema, entries map[string]Entry, from, to time.Time) map[string]*rawFilm {
	raws := map[string]*rawFilm{}
	for _, c := range cinemas {
		for _, x := range entries[c.Name].Shows {
			if !scrape.InWindow(x.Start, from, to) {
				continue
			}
			if _, err := time.Parse(scrape.TimeFmt, x.Start); err != nil {
				continue
			}
			title, _ := CleanTitle(html.UnescapeString(x.Film))
			k := FilmKey(title)
			r := raws[k]
			if r == nil {
				r = &rawFilm{key: k, votes: map[string]int{}, durs: map[string]int{}}
				raws[k] = r
			}
			r.votes[title]++
			r.shows++
			if x.Duration != 0 {
				if _, ok := r.durs[c.Name]; !ok {
					r.durs[c.Name] = x.Duration
				}
			}
		}
	}
	for _, r := range raws {
		for _, t := range sortedKeys(r.votes) {
			if r.title == "" || r.votes[t] > r.votes[r.title] {
				r.title = t
			}
		}
		var tags []string
		r.match, r.base, tags = matchKey(r.title)
		r.talk = slices.Contains(tags, "Q&A")
	}
	return raws
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// resolved is the outcome of matching: each listing's final key, and what the page is told.
type resolved struct {
	final   map[string]string // ordinary key -> film key
	aliases map[string]string
	notes   map[string]FilmNote
	titles  map[string]string // film key -> display title override
}

// resolve groups listings: by title rule or film database, split on very different runtimes,
// then the owner's saved decisions on top.
func resolve(raws map[string]*rawFilm, m *films.Snapshot) *resolved {
	group := map[string][]*rawFilm{}
	final := map[string]string{}
	for _, k := range sortedKeys(raws) {
		r := raws[k]
		if m.Merge[k] == k {
			final[k], r.decided = k, true
			continue
		}
		g := r.match
		if l, ok := m.Lookups[r.match]; ok && l.ID != 0 {
			g = l.Key
		}
		group[g] = append(group[g], r)
	}
	for g, rs := range group {
		anchor := rs[0]
		for _, r := range rs {
			if r.runtime() != 0 && (anchor.runtime() == 0 || r.shows > anchor.shows) {
				anchor = r
			}
		}
		var apart []*rawFilm
		kept := rs[:0:0]
		for _, r := range rs {
			if a, b := anchor.runtime(), r.runtime(); a != 0 && b != 0 && max(a-b, b-a) > runtimeGap {
				apart = append(apart, r)
			} else {
				kept = append(kept, r)
			}
		}
		key := g
		for _, r := range apart {
			final[r.key] = r.key
			if r.key == g { // don't let the rest of the group share its key
				key = anchor.key
			}
		}
		for _, r := range kept {
			final[r.key] = key
		}
	}

	// The owner's merges, which may name a listing or a whole film.
	follow := func(k string) string {
		for seen := map[string]bool{}; !seen[k]; {
			seen[k] = true
			if t, ok := m.Merge[k]; ok && t != k {
				k = t
				continue
			}
			if f, ok := final[k]; ok && f != k {
				k = f
			}
		}
		return k
	}
	merged := map[string]bool{}
	for _, k := range sortedKeys(raws) {
		if t := follow(k); t != final[k] {
			final[k], merged[t], raws[k].decided = t, true, true
		}
	}

	res := &resolved{final: final, aliases: map[string]string{}, notes: map[string]FilmNote{}, titles: map[string]string{}}
	members := map[string][]*rawFilm{}
	for _, k := range sortedKeys(raws) {
		r, f := raws[k], final[k]
		members[f] = append(members[f], r)
		if k != f {
			res.aliases[k] = f
		}
		if r.match != f && !r.decided {
			res.aliases[r.match] = f // a pick saved under an older match key
		}
	}
	shown := map[string]string{} // plain titles: what suggestions compare
	for f, rs := range members {
		shown[f] = rs[0].base
		if len(rs) == 1 {
			continue
		}
		names := make(map[string]string, len(rs))
		sure := merged[f] || m.Confirmed[f]
		id := -1
		for _, r := range rs {
			names[r.key] = r.title
			l := m.Lookups[r.match]
			if id == -1 {
				id = l.ID
			}
			if l.ID != id {
				id = 0
			}
		}
		if id > 0 {
			sure = true
		}
		res.notes[f] = FilmNote{Names: names, U: !sure}
		res.titles[f] = plainTitle(rs, m)
		shown[f] = res.titles[f]
	}

	// Close titles that didn't merge are offered as suggestions.
	similar := map[string][]string{}
	for _, p := range films.SimilarIndex(shown) {
		a, b := p[0], p[1]
		if !m.Apart[films.PairKey(a, b)] && (films.Key(shown[a]) == films.Key(shown[b]) || films.Similar(shown[a], shown[b])) {
			if len(similar[a]) < maxSimilar {
				similar[a] = append(similar[a], b)
			}
			if len(similar[b]) < maxSimilar {
				similar[b] = append(similar[b], a)
			}
		}
	}
	for f, s := range similar {
		n := res.notes[f]
		n.S = s
		for _, o := range s {
			n.U = n.U || (!m.Confirmed[f] && !films.LanguageOnly(shown[f], shown[o]))
		}
		res.notes[f] = n
	}
	return res
}

// plainTitle names a merged film: the database's title, else the most used title with nothing
// extra on it, else the plain form of the most used one.
func plainTitle(rs []*rawFilm, m *films.Snapshot) string {
	var best *rawFilm
	for _, r := range rs {
		if l := m.Lookups[r.match]; l.ID != 0 {
			return l.Title
		}
		if r.base == r.title && (best == nil || r.shows > best.shows) {
			best = r
		}
	}
	if best != nil {
		return best.title
	}
	best = rs[0]
	for _, r := range rs {
		if r.shows > best.shows {
			best = r
		}
	}
	return best.base
}
