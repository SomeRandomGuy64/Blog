package ticketing

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"fazon.nexus/cinema/internal/scrape"
)

// instance attribute name (lower-case, "attribute_" stripped) -> tag
var instAttrTags = map[string]string{
	"audiodescription": "Audio Described", "audiodescribed": "Audio Described",
	"accessaudiodescribed": "Audio Described",
	"captionsubtitled":     "Subtitled", "captioned": "Subtitled", "subtitled": "Subtitled",
	"accesssubtitled": "Subtitled",
	"relaxed":         "Relaxed", "accessrelaxed": "Relaxed", "relaxedperformance": "Relaxed",
	"parentandbaby": "Parent & Baby",
	"bsl":           "BSL", "bslinterpreted": "BSL", "bslled": "BSL",
	"dubbed":           "Dubbed",
	"dementiafriendly": "Dementia Friendly", "accessdementiafriendly": "Dementia Friendly",
	"silverscreening": "Silver Screening",
	"preview":         "Preview",
}

var strandTags = [][2]string{{"baby kino", "Parent & Baby"}, {"relaxed", "Relaxed"},
	{"dementia", "Dementia Friendly"}, {"silver", "Silver Screening"}}

var nameTags = []rule{
	{ci(`q\s*&\s*a|q and a`), "Q&A"}, {ci(`\b35\s?mm\b`), "35mm"}, {ci(`\b70\s?mm\b`), "70mm"},
	{ci(`\bimax\b`), "IMAX"}, {ci(`\b3d\b`), "3D"}, {ci(`\b4k\b`), "4K"}, {ci(`\bpreview\b`), "Preview"},
	{ci(`\brelaxed\b`), "Relaxed"}, {ci(`baby kino|parent\s*(?:&|and)\s*baby`), "Parent & Baby"},
	{ci(`sing-?a-?long`), "Sing-along"}, {ci(`dog friendly`), "Dog Friendly"},
	{ci(`dementia friendly`), "Dementia Friendly"}, {ci(`\bdubbed\b`), "Dubbed"},
	{ci(`^ntl\b|^eos\b|nt live|national theatre live|royal (?:opera|ballet)|met opera|rsc live|` +
		`exhibition on screen|bolshoi|\blive:|live screening`), "Live/Event Cinema"},
}

func ci(p string) *regexp.Regexp { return regexp.MustCompile("(?i)" + p) }

var (
	eventLiveRe = ci(`stage on screen|live screening|alternative content|^theatre$`)
	prefixRe    = ci(`^(?:LFF\d*|BFI LFF|Presents|Early Doors|ED|Baby Kino|Dog Friendly|Classics|Spotlight|Freshers|` +
		`Family Time|TK|Perspectives|UK Premiere|Cinema Social|Reel Queer|Relaxed|Preview|` +
		`Parent (?:&|and) Baby|Dementia Friendly|EOS|Opening Night(?: Film| Gala)?|Closing Night(?: Film| Gala)?)\s*[:/]\s*` +
		`|^MLF presents\s+`)
	notScreeningRe = ci(`\bpass\b|private event|do not sell|secure my booking|gift voucher|snack pack|membership`)
	certRe         = ci(`\s*\((?:U|PG|12A?|15|18|R18|TBC)\)`)
	suffixRe       = ci(`\s*\+\s*(?:recorded\s+)?(?:q\s*&\s*a|q and a|discussion|intro\w*|panel\w*|talk)\b.*$` +
		`|\s*\[[^\]]*\]\s*$`)
	ntPrefixRe = ci(`^(?:NTL|NT Live|National Theatre Live)\s*:\s*`)
	qaRe       = ci(`q\s*&\s*a`)
	leadNumRe  = regexp.MustCompile(`^(\d+)`)
)

// cleanTitle strips Spektrix strand prefixes, "+ Q&A" style suffixes and certificates.
func cleanTitle(name string) string {
	t := scrape.Squash(name)
	t = ntPrefixRe.ReplaceAllString(t, "NT Live: ")
	for range 3 {
		t = prefixRe.ReplaceAllString(t, "")
	}
	t = suffixRe.ReplaceAllString(t, "")
	t = certRe.ReplaceAllString(t, "")
	if t = strings.Trim(t, " -:"); t != "" {
		return t
	}
	return strings.TrimSpace(name)
}

// falsy attribute values (Python `v in (None, False, "", "False", "false")`, where 0 == False).
func attrUnset(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case bool:
		return !x
	case string:
		return x == "" || x == "False" || x == "false"
	case float64:
		return x == 0
	}
	return false
}

func spxFormats(ev map[string]any, inst obj) []string {
	var tags []string
	for _, k := range inst.keys {
		v := inst.m[k]
		if !strings.HasPrefix(k, "attribute_") || attrUnset(v) {
			continue
		}
		key := strings.ToLower(k[len("attribute_"):])
		sv, isStr := v.(string)
		if t, ok := instAttrTags[key]; ok {
			tags = append(tags, t)
		} else if key == "additionalevent" && isStr {
			if qaRe.MatchString(sv) {
				tags = append(tags, "Q&A")
			} else {
				tags = append(tags, headRunes(strings.TrimSpace(strings.TrimLeft(sv, "+ ")), 30))
			}
		} else if key == "strand" && isStr {
			for _, st := range strandTags {
				if strings.Contains(strings.ToLower(sv), st[0]) {
					tags = append(tags, st[1])
				}
			}
		}
	}
	name := str(ev, "name")
	for _, r := range nameTags {
		if r.re.MatchString(name) {
			tags = append(tags, r.tag)
		}
	}
	for k, v := range ev {
		if sv, ok := v.(string); ok && strings.HasPrefix(k, "attribute_") && eventLiveRe.MatchString(strings.TrimSpace(sv)) {
			tags = append(tags, "Live/Event Cinema")
			break
		}
	}
	out := []string{}
	for _, t := range scrape.Uniq(tags) {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// spektrixVenue configures one Spektrix v3 client.
type spektrixVenue struct {
	client string
	isFilm func(ev map[string]any) bool
	// urlTpl is formatted with {id} (full v3 instance id) and {num} (its numeric web id).
	urlTpl string
	// screen(event, instance, planName); nil = the seating plan's name.
	screen func(ev map[string]any, inst obj, plan string) string
	// slot: the event `duration` is the venue's whole slot incl. ads rather than the runtime
	// (HOME/Showroom list Sense and Sensibility, 132 min, as 157/152), i.e. start + duration = end.
	slot bool
}

func (v spektrixVenue) fetch(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	base := "https://system.spektrix.com/" + v.client + "/api/v3"
	var events []map[string]any
	if err := s.GetJSON(base+"/events", &events, scrape.Query(map[string][]string{
		"instanceStart_from": {from.Format(scrape.DateFmt) + "T00:00:00"},
		"instanceStart_to":   {to.AddDate(0, 0, 1).Format(scrape.DateFmt) + "T00:00:00"},
	})); err != nil {
		return nil, fmt.Errorf("Spektrix %s: %w", v.client, err)
	}
	urlTpl := v.urlTpl
	if urlTpl == "" {
		urlTpl = "https://system.spektrix.com/" + v.client + "/website/ChooseSeats.aspx?EventInstanceId={num}"
	}
	lo, hi := from.Format(scrape.DateFmt), to.Format(scrape.DateFmt)
	plans := map[string]string{}
	seen := map[[2]string]bool{}
	var out []scrape.Show
	for _, ev := range events {
		if !v.isFilm(ev) || notScreeningRe.MatchString(str(ev, "name")) {
			continue
		}
		evID := fmt.Sprint(ev["id"])
		var insts []obj
		if err := s.GetJSON(base+"/events/"+evID+"/instances", &insts); err != nil {
			return nil, err
		}
		// on-sale first, so an off-sale duplicate placeholder at the same time is dropped
		sort.SliceStable(insts, func(i, j int) bool {
			return truthy(insts[i].m["isOnSale"]) && !truthy(insts[j].m["isOnSale"])
		})
		for _, inst := range insts {
			st := str(inst.m, "start")
			if len(st) > 16 {
				st = st[:16]
			}
			day := st
			if len(day) > 10 {
				day = day[:10]
			}
			if day < lo || day > hi || seen[[2]string{evID, st}] {
				continue
			}
			if truthy(inst.m["cancelled"]) || inst.m["attribute_Cancelled"] == true {
				continue
			}
			seen[[2]string{evID, st}] = true
			pid := str(inst.m, "planId")
			if _, ok := plans[pid]; pid != "" && !ok {
				var p struct{ Name string }
				plans[pid] = ""
				if s.GetJSON(base+"/plans/"+pid, &p) == nil {
					plans[pid] = strings.TrimSpace(p.Name)
				}
			}
			pname := plans[pid]
			screen := pname
			if v.screen != nil {
				screen = v.screen(ev, inst, pname)
			}
			id := str(inst.m, "id")
			num := id
			if m := leadNumRe.FindString(id); m != "" {
				num = m
			}
			sh := scrape.Show{
				Film: cleanTitle(str(ev, "name")), Start: st, Screen: screen, Formats: spxFormats(ev, inst),
				URL: strings.NewReplacer("{id}", id, "{num}", num).Replace(urlTpl),
			}
			dur := scrape.ToInt(ev["duration"])
			if !v.slot {
				sh.Duration = dur
			} else if t, err := time.Parse(scrape.TimeFmt, st); err == nil && dur != 0 {
				sh.End = t.Add(time.Duration(dur) * time.Minute).Format(scrape.TimeFmt)
			}
			out = append(out, sh)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].Film < out[j].Film
	})
	return out, nil
}

func strOr(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

var homeVenue = spektrixVenue{
	client: "home",
	isFilm: func(e map[string]any) bool {
		return str(e, "attribute_ShowType") == "Film" || str(e, "attribute_IlluminateEventType") == "Screening"
	},
	urlTpl: "https://www.homemcr.org/spektrix/ChooseSeats?EventInstanceId={id}",
	screen: func(e map[string]any, i obj, p string) string {
		return strings.TrimSpace(strOr(str(i.m, "attribute_Screen"), p))
	},
	slot: true,
}

var showroomVenue = spektrixVenue{
	client: "showroomworkstation",
	isFilm: func(e map[string]any) bool {
		return !truthy(e["attribute_ShowroomNonFilmEvent"]) && !truthy(e["attribute_WorkStationEvent"])
	},
	urlTpl: "https://tickets.showroomcinema.org.uk/showroomworkstation/website/ChooseSeats.aspx?EventInstanceId={num}",
	slot:   true,
}

var storyhouseVenue = spektrixVenue{
	client: "storyhousechester",
	isFilm: func(e map[string]any) bool {
		return str(e, "attribute_EventType") == "Cinema" || strings.HasPrefix(str(e, "attribute_ShowType"), "Cinema")
	},
}

var garrickFilmRe = ci(`theatre live|\bscreening\b`)

var garrickVenue = spektrixVenue{
	client: "altrinchamgarrick",
	isFilm: func(e map[string]any) bool {
		return str(e, "attribute_Genre") == "Live screening" ||
			str(e, "attribute_ProductionCategory") == "Garrick Picturehouse" ||
			garrickFilmRe.MatchString(str(e, "name"))
	},
	urlTpl: "https://tickets.altrinchamgarrick.co.uk/altrinchamgarrick/website/ChooseSeats.aspx?EventInstanceId={num}",
	screen: func(e map[string]any, i obj, p string) string { return strOr(str(e, "attribute_Venue"), p) },
}

var buxtonVenue = spektrixVenue{
	client: "buxtonoperahouse",
	isFilm: func(e map[string]any) bool {
		return str(e, "attribute_ADUKClassifierTier1") == "Film" || str(e, "attribute_EventCategory") == "Film" ||
			str(e, "attribute_Programme") == "Buxton Cinema"
	},
	urlTpl: "https://buxtonoperahouse.org.uk/book/instance/{num}/",
	screen: func(e map[string]any, i obj, p string) string { return strOr(str(e, "attribute_Venue"), p) },
}

var dukesVenue = spektrixVenue{
	client: "dukeslancaster",
	isFilm: func(e map[string]any) bool { return str(e, "attribute_CinemaOrStage") == "Cinema" },
	urlTpl: "https://dukeslancaster.org/book-now?id={num}",
}

func HomeManchester(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	return homeVenue.fetch(s, from, to)
}

func ShowroomSheffield(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	return showroomVenue.fetch(s, from, to)
}

func StoryhouseChester(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	return storyhouseVenue.fetch(s, from, to)
}

func AltrinchamGarrick(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	return garrickVenue.fetch(s, from, to)
}

func BuxtonOperaHouse(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	return buxtonVenue.fetch(s, from, to)
}

func DukesLancaster(s *scrape.Session, from, to time.Time) ([]scrape.Show, error) {
	return dukesVenue.fetch(s, from, to)
}
