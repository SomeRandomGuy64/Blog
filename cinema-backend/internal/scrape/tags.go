package scrape

import (
	"regexp"
	"strings"
	"unicode"
)

// Format tags: every source names things differently, so squash them into one vocabulary.

type canon struct {
	re    *regexp.Regexp
	label string
}

func ci(p string) *regexp.Regexp { return regexp.MustCompile("(?i)" + p) }

var canonTags = []canon{
	{ci(`1\.43`), "IMAX 1.43:1"}, {ci(`imax`), "IMAX"}, {ci(`4dx`), "4DX"}, {ci(`screen ?x`), "ScreenX"},
	{ci(`\b3d\b`), "3D"}, {ci(`isense`), "iSense"}, {ci(`^dolby$|dolby ?cinema`), "Dolby Cinema"},
	{ci(`atmos`), "Dolby Atmos"}, {ci(`xplus|x ?plus`), "XPlus"}, {ci(`infinity`), "Infinity Vision"},
	{ci(`super ?screen`), "Superscreen"}, {ci(`70 ?mm`), "70mm"}, {ci(`35 ?mm`), "35mm"}, {ci(`\bepic\b`), "EPIC"},
	{ci(`d-?box`), "D-BOX"}, {ci(`ultra ?lux`), "Ultra Lux"}, {ci(`luxe`), "Luxe"}, {ci(`\b4k\b`), "4K"},
	{ci(`subtitl|sub ?cinema|hohsub|caption|\(sub\)|^subbed$`), "Subtitled"}, {ci(`dubbed|dub cinema`), "Dubbed"},
	{ci(`relaxed|sensory|autism`), "Relaxed"}, {ci(`baby`), "Parent & Baby"}, {ci(`silver`), "Silver Screen"},
	{ci(`toddler`), "Toddler Time"}, {ci(`dementia`), "Dementia Friendly"}, {ci(`q ?[+&] ?a`), "Q&A"},
	{ci(`event|livesat|\blive\b|encore|screen ?art|alt ?content`), "Event Cinema"},
	{ci(`classic|throwback|re-?discover|back on the big|rephouse`), "Classic"},
	{ci(`preview|premiere`), "Preview"},
}

var noiseRe = ci(`wheelchair|flash|strobe|audio ?d|^ad$|allocated|reserved|single seat|^2d$|digital|^english|saver|£|` +
	`\bweb\b|certificate|almost ?sold|recliner|^kids|odeon kids|over 18|ad-trailer|priority|prerecorded|` +
	`^plf$|^standard$|pre ?recorded|special pricing|laser|lux and lux|hdr by barco|biggest screen`)

var langRe = ci(`^(hindi|tamil|telugu|malayalam|punjabi|polish|korean|japanese|mandarin|cantonese|` +
	`arabic|french|spanish|italian|german|kannada|bengali|urdu|gujarati|marathi)\b`)

// Premium lists the formats the page highlights.
var Premium = []string{"IMAX", "IMAX 1.43:1", "4DX", "ScreenX", "3D", "iSense", "Dolby Cinema", "Dolby Atmos",
	"XPlus", "Infinity Vision", "Superscreen", "70mm", "35mm", "EPIC", "D-BOX", "Luxe", "Ultra Lux", "4K"}

// Tag normalises one source label ("" = drop). strict drops anything unrecognised, for
// sources whose labels are mostly marketing "seasons".
func Tag(raw string, strict bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || noiseRe.MatchString(raw) {
		return ""
	}
	for _, c := range canonTags {
		if c.re.MatchString(raw) {
			return c.label
		}
	}
	if m := langRe.FindStringSubmatch(raw); m != nil {
		return pyTitle(m[1])
	}
	if strict {
		return ""
	}
	return raw
}

// Tags normalises and de-duplicates labels, keeping first-seen order.
func Tags(raws []string, strict bool) []string {
	out := []string{}
	for _, r := range raws {
		if t := Tag(r, strict); t != "" && !contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// Uniq de-duplicates preserving order (dict.fromkeys).
func Uniq(xs []string) []string {
	out := []string{}
	for _, x := range xs {
		if !contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// pyTitle is str.title() for a single word: first letter upper, rest lower.
func pyTitle(w string) string {
	r := []rune(strings.ToLower(w))
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// Camel splits "SubtitledCinema" into "Subtitled Cinema" (a space at each ASCII lower→Upper boundary).
func Camel(s string) string {
	var b strings.Builder
	var prev rune
	for i, r := range s {
		if i > 0 && 'a' <= prev && prev <= 'z' && 'A' <= r && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}
