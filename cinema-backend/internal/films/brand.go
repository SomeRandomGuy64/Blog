package films

import (
	"regexp"
	"strings"
)

// Event cinema (theatre, ballet and opera screened in cinemas) is listed under many names for
// one brand: "NT Live", "National Theatre Live" and "National Theatre" are the same company, and
// cinemas add seasons ("2026-27"), "Live" or "Encore" and leads like "On Screen - ". The brand is
// kept in the title, so the Royal Ballet's Nutcracker never meets another company's Nutcracker,
// or NT Live's Golden Boy the 1939 film; only the way it is written is made the same.

type eventBrand struct {
	name string // how the brand is written in the base title
	re   string // the ways cinemas write it before the show, lower case, longest first
	tail string // the ways it may be written after the show, where a bare "The Met" is too loose
}

var eventBrands = []eventBrand{
	{"NT Live", `national theatre live|national theatre at home|national theatre|nt ?live|nt at home`, ""},
	{"Royal Ballet & Opera", `the royal ballet (?:&|and) opera|royal ballet (?:&|and) opera|royal opera house|the royal ballet|the royal opera|royal ballet|royal opera|rbo|roh`, ""},
	{"Met Opera", `the metropolitan opera|metropolitan opera|the met opera|met opera|the met(?: live in hd)?|met`, `the metropolitan opera|metropolitan opera|the met opera|met opera`},
}

const (
	// what may come between a brand and the show: seasons, "Live", "Encore", "Cinema Season"
	brandExtra = `(?:(?:\s*[:\-–]\s*|\s+)(?:\d{2,4}(?:\s*[-/–]\s*\d{2,4})?|live(?:\s+in\s+hd)?|encore|cinema|season|screenings?|in\s+hd))*`
	// a short lead before the brand, like "On Screen - " or "Christmas 26 - "
	brandLead = `(?:[\pL\d']+(?:\s+[\pL\d']+){0,2}\s*[-–|]\s*)?`
)

var (
	brandPrefix  []*regexp.Regexp
	brandSuffix  []*regexp.Regexp
	brandBracket = regexp.MustCompile(`(?i)\s*[\(\[]\s*(?:\d{2,4}(?:\s*[-/–]\s*\d{2,4})?|live|encore|cert(?:ificate)?|u|pg|12a?|15|18|tbc)(?:[\s,]+(?:\d{2,4}(?:\s*[-/–]\s*\d{2,4})?|live|encore|cert(?:ificate)?|u|pg|12a?|15|18|tbc))*\s*[\)\]]`)
)

func init() {
	for _, b := range eventBrands {
		tail := b.tail
		if tail == "" {
			tail = b.re
		}
		brandPrefix = append(brandPrefix, regexp.MustCompile(`(?i)^`+brandLead+`(?:`+b.re+`)`+brandExtra+`\s*[:\-–|]\s*`))
		brandSuffix = append(brandSuffix, regexp.MustCompile(`(?i)(?:\s*[:\-–|]\s*|\s+)(?:`+tail+`)`+brandExtra+`\s*$`))
	}
}

// Event reports whether a title is event cinema from a known brand. Its runtime says little
// (cinemas differ on counting the interval), so it isn't used to split a match.
func Event(title string) bool {
	_, _, ok := eventShow(strings.TrimSpace(spaceColon.ReplaceAllString(title, ":")))
	return ok
}

// eventShow splits an event cinema title into its brand and show: "On Screen - NT Live: Golden
// Boy (Cert 12A)" is NT Live and "Golden Boy (Cert 12A)", and "Samson et Dalila - Met Opera
// 26/27" is Met Opera and "Samson et Dalila". Brands written twice, as in "RBO 2026-27: The Royal
// Ballet - The Nutcracker", are taken off together. ok is false for any other title.
func eventShow(title string) (brand, show string, ok bool) {
	s := title
	for i, re := range brandPrefix {
		if t := re.ReplaceAllString(s, ""); t != s && strings.TrimSpace(t) != "" {
			brand, s = eventBrands[i].name, t
			if t := re.ReplaceAllString(s, ""); t != s && strings.TrimSpace(t) != "" { // the brand again, written another way
				s = t
			}
			break
		}
	}
	if brand == "" {
		for i, re := range brandSuffix {
			if t := re.ReplaceAllString(s, ""); t != s && strings.TrimSpace(t) != "" {
				brand, s = eventBrands[i].name, t
				break
			}
		}
	}
	if brand == "" {
		return "", title, false
	}
	s = strings.TrimSpace(brandBracket.ReplaceAllString(s, ""))
	return brand, s, s != ""
}
