// Package films works out when differently named listings are the same film: title rules, a
// saved list of decisions (Store) and an optional film database (TMDB).
package films

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	prefixThrowback = regexp.MustCompile(`(?i)^throwback\s*:\s*`)
	prefixPresents  = regexp.MustCompile(`(?i)^[^:|]{1,40}\bpresents?\s*:\s*`)
	suffixQA        = regexp.MustCompile(`(?i)\s*[+&–-]\s*(?:[\w'-]+\s+){0,3}q ?(?:&|and|\+) ?a(?:\s+[\w'-]+){0,3}\s*$`)
	bracketed       = regexp.MustCompile(`\s*[\(\[]([^()\[\]]*)[\)\]]`)
	decoWord        = regexp.MustCompile(`(?i)^(?:\d+(?:st|nd|rd|th)|anniversary|4k|restoration|restored|remaster(?:ed)?|ctba|preview|re-?release)$`)
	anniversary     = regexp.MustCompile(`(?i)\b\d+(?:st|nd|rd|th)\s+anniversary\b`)
	cert            = regexp.MustCompile(`(?i)^cert(?:ificate)?\s+\S+$`)
	has4K           = regexp.MustCompile(`(?i)\b4k\b`)
	preview         = regexp.MustCompile(`(?i)\bpreview\b`)
	spaceColon      = regexp.MustCompile(`\s+:`)
	trailingPunct   = regexp.MustCompile(`[\s:,;–-]+$`)
)

// decoration reports whether a bracket's contents only describe the release, not the film.
// Language labels and anything else unknown are not decoration.
func decoration(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if cert.MatchString(s) {
		return true
	}
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == ',' }) {
		if !decoWord.MatchString(w) {
			return false
		}
	}
	return true
}

// Base strips the parts of a cleaned title that name a release rather than a film, and returns
// the tags they implied: "Throwback: Rocky (50th Anniversary 4K)" is Rocky, Classic and 4K.
// Language labels, double bills and extras like "+ draw-along" stay, so those listings stay apart.
func Base(title string) (string, []string) {
	var tags []string
	add := func(t string) {
		if !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	s := strings.TrimSpace(spaceColon.ReplaceAllString(title, ":"))
	if t := prefixThrowback.ReplaceAllString(s, ""); t != s {
		add("Classic")
		s = t
	}
	s = prefixPresents.ReplaceAllString(s, "")
	if t := suffixQA.ReplaceAllString(s, ""); t != s {
		add("Q&A")
		s = t
	}
	if anniversary.MatchString(s) {
		add("Classic")
	}
	if has4K.MatchString(s) {
		add("4K")
	}
	s = bracketed.ReplaceAllStringFunc(s, func(m string) string {
		in := bracketed.FindStringSubmatch(m)[1]
		if !decoration(in) {
			return m
		}
		if preview.MatchString(in) {
			add("Preview")
		}
		return ""
	})
	s = trimDecoration(s)
	s = strings.Join(strings.Fields(trailingPunct.ReplaceAllString(s, "")), " ")
	if s == "" {
		return strings.TrimSpace(title), nil
	}
	return s, tags
}

// trimDecoration removes trailing words like "50th Anniversary 4K". The run must hold a 4K or
// an "Nth Anniversary", so "The Anniversary" and "The Restoration" keep their titles.
func trimDecoration(s string) string {
	w := strings.Fields(s)
	cut := len(w)
	for cut > 0 && decoWord.MatchString(w[cut-1]) {
		cut--
	}
	if cut == len(w) || cut == 0 {
		return s
	}
	tail := strings.Join(w[cut:], " ")
	if !has4K.MatchString(tail) && !anniversary.MatchString(tail) {
		return s
	}
	return strings.Join(w[:cut], " ")
}

// Fold removes accents, so "André" and "Andre" or "żyć" and "zyc" compare equal.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'ł' || r == 'Ł':
			b.WriteRune('l')
		case r == 'ø' || r == 'Ø':
			b.WriteRune('o')
		case r == 'ß':
			b.WriteString("ss")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Key is the grouping key for a base title. It is "" for titles with no Latin letters or
// digits, which then keep their ordinary film key.
func Key(t string) string {
	t = strings.ReplaceAll(strings.ToLower(Fold(t)), "&", "and")
	t = strings.TrimPrefix(t, "the ")
	t = partWord.ReplaceAllStringFunc(t, func(m string) string { return "part" + partNumber[partWord.FindStringSubmatch(m)[1]] })
	var b strings.Builder
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	partWord   = regexp.MustCompile(`\bpart (one|two|three|four|ii|iii|iv)\b`)
	partNumber = map[string]string{"one": "1", "two": "2", "three": "3", "four": "4", "ii": "2", "iii": "3", "iv": "4"}
)

var sequelWord = map[string]bool{"part": true, "chapter": true, "volume": true, "vol": true, "episode": true,
	"ii": true, "iii": true, "iv": true, "vi": true, "two": true, "three": true, "four": true}

func words(t string) []string {
	t = strings.ReplaceAll(strings.ToLower(Fold(t)), "&", "and")
	return strings.FieldsFunc(t, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
}

func digitsOf(w []string) []string {
	var d []string
	for _, x := range w {
		if strings.IndexFunc(x, unicode.IsDigit) >= 0 {
			d = append(d, x)
		}
	}
	return d
}

// wordsWithin finds s as a run of words inside l, returning where, or -1.
func wordsWithin(s, l []string) int {
	for i := 0; i+len(s) <= len(l); i++ {
		if slices.Equal(s, l[i:i+len(s)]) {
			return i
		}
	}
	return -1
}

// Similar reports whether two titles are close enough to offer as possibly the same film. It
// is a suggestion, not a merge, so it may be wrong, but sequels should not show up: "Dune" and
// "Dune: Part 3" differ in number, and "Wicked: For Good" is a subtitle added after a colon.
func Similar(a, b string) bool {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 || !slices.Equal(digitsOf(wa), digitsOf(wb)) {
		return false
	}
	short, long, longT := wa, wb, b
	if len(wa) > len(wb) {
		short, long, longT = wb, wa, a
	}
	if len(short) == len(long) {
		return oneTypo(wa, wb)
	}
	if len(strings.Join(short, "")) < 6 {
		return false
	}
	at := wordsWithin(short, long)
	// In the middle of a longer title it's a coincidence: "Michael" in "George Michael: The Faith Tour".
	if at < 0 || (at != 0 && at+len(short) != len(long)) || len(long)-len(short) > 6 {
		return false
	}
	for _, w := range long {
		if sequelWord[w] && !slices.Contains(short, w) {
			return false
		}
	}
	if at == 0 {
		// What follows the shorter title: a colon or dash starts a subtitle, so a different film.
		lower := strings.ToLower(Fold(longT))
		if i := strings.Index(lower, short[len(short)-1]); i >= 0 {
			rest := strings.TrimLeft(lower[i+len(short[len(short)-1]):], " ")
			if strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, "–") {
				return false
			}
		}
	}
	return true
}

// oneTypo reports whether two equally long titles differ in one real word, by a letter or two.
func oneTypo(a, b []string) bool {
	diff := 0
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		diff++
		if diff > 1 || min(len(a[i]), len(b[i])) < 4 || editDistance(a[i], b[i]) > 2 {
			return false
		}
	}
	return diff == 1
}

var languageWord = map[string]bool{"language": true, "version": true, "hindi": true, "tamil": true, "telugu": true,
	"malayalam": true, "punjabi": true, "polish": true, "korean": true, "japanese": true, "mandarin": true, "cantonese": true,
	"arabic": true, "french": true, "spanish": true, "italian": true, "german": true, "kannada": true, "bengali": true,
	"urdu": true, "gujarati": true, "marathi": true}

// LanguageOnly reports whether two titles differ only by a language label. These are kept
// apart on purpose, so they are offered as suggestions without a badge.
func LanguageOnly(a, b string) bool {
	wa, wb := words(a), words(b)
	if len(wa) > len(wb) {
		wa, wb = wb, wa
	}
	if len(wa) == len(wb) || wordsWithin(wa, wb) < 0 {
		return false
	}
	for _, w := range wb {
		if !languageWord[w] && !slices.Contains(wa, w) {
			return false
		}
	}
	return true
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			c := 1
			if ra[i-1] == rb[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// SimilarIndex returns candidate pairs of keys for Similar, so a big listing doesn't compare
// every film with every other: only films sharing an uncommon word are paired.
func SimilarIndex(titles map[string]string) [][2]string {
	byWord := map[string][]string{}
	for k, t := range titles {
		seen := map[string]bool{}
		for _, w := range words(t) {
			if len(w) >= 4 && !seen[w] {
				seen[w] = true
				byWord[w] = append(byWord[w], k)
			}
		}
	}
	seen := map[[2]string]bool{}
	var out [][2]string
	for _, ks := range byWord {
		if len(ks) < 2 || len(ks) > 24 {
			continue
		}
		slices.Sort(ks)
		for i, a := range ks {
			for _, b := range ks[i+1:] {
				if p := [2]string{a, b}; !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
	}
	slices.SortFunc(out, func(x, y [2]string) int {
		if c := strings.Compare(x[0], y[0]); c != 0 {
			return c
		}
		return strings.Compare(x[1], y[1])
	})
	return out
}
