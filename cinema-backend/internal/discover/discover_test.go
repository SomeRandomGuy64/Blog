package discover

import (
	"errors"
	"slices"
	"testing"

	"fazon.nexus/cinema/internal/chainfile"
)

func TestMatchSlug(t *testing.T) {
	slugs := []string{"liverpool-one", "manchester-trafford-centre", "dudley-merry-hill", "bournemouth-bh2",
		"oxford-george-st", "leeds-bradford", "bradford-extra-long"}
	for _, c := range []struct {
		name, want string
		ok         bool
	}{
		{"Liverpool One", "liverpool-one", true},
		{"Trafford Centre", "manchester-trafford-centre", true},
		{"Dudley", "dudley-merry-hill", true},
		{"Bournemouth BH2", "bournemouth-bh2", true},
		{"Bradford", "leeds-bradford", true}, // shortest containing slug
		{"Ford", "ford", false},              // "ford" is not a whole word of any slug
		{"Point Square", "point-square", false},
	} {
		got, ok := matchSlug(c.name, slugs)
		if got != c.want || ok != c.ok {
			t.Errorf("matchSlug(%q) = %q, %v; want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestSlugifyPostcode(t *testing.T) {
	if got := slugify("Duke's at Komedia & Co."); got != "duke-s-at-komedia-co" {
		t.Errorf("slugify = %q", got)
	}
	for in, want := range map[string]string{"Canterbury, CT1 1BN": "CT1 1BN", "london, sw1e6qw ": "SW1E 6QW",
		"Bournemouth BH2": "", "L1 4DQ": "L1 4DQ", "EC1A 1BB x M3 4EN": "M3 4EN"} {
		if got := findPostcode(in); got != want {
			t.Errorf("findPostcode(%q) = %q, want %q", in, got, want)
		}
	}
	if got := joinAddress("2 Canter Way,", "London,", "E1 8PS"); got != "2 Canter Way, London" {
		t.Errorf("joinAddress = %q", got)
	}
	if got := joinAddress("O2 Centre, London, 51.548, -0.1818", "Mile End, Essex CO4 5UP"); got != "O2 Centre, London, Mile End, Essex" {
		t.Errorf("joinAddress = %q", got)
	}
	if got := joinAddress("Unit 9,", "The Riverside,", "Canterbury, CT1 1BN"); got != "Unit 9, The Riverside, Canterbury" {
		t.Errorf("joinAddress = %q", got)
	}
}

func TestChainName(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"Cineworld", "London - Wembley"}:  "Cineworld London Wembley",
		{"Everyman", "Everyman Leeds"}:     "Everyman Leeds",
		{"Odeon", "Birmingham New Street"}: "Odeon Birmingham New Street",
		{"Reel", "Fareham Hampshire "}:     "Reel Fareham Hampshire",
	} {
		if got := chainName(in[0], in[1]); got != want {
			t.Errorf("chainName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestFindMovieTheater(t *testing.T) {
	body := []byte(`<html><head>
<script type="application/ld+json">{"@type":"WebSite","name":"x"}</script>
<script data-rh="true" type="application/ld+json">{"@context":"https://schema.org","@type":"MovieTheater","name":"Leeds",
"address":{"@type":"PostalAddress","streetAddress":"Gelderd Road","addressLocality":"Batley","postalCode":"WF17 9TB","addressCountry":"GB"},
"geo":{"@type":"GeoCoordinates","latitude":53.7441,"longitude":"-1.6401"}}</script></head></html>`)
	mt, ok := findMovieTheater(body)
	if !ok {
		t.Fatal("no MovieTheater")
	}
	var e chainfile.Entry
	fillTheater(&e, "Showcase", mt)
	want := chainfile.Entry{Name: "Showcase Leeds", Lat: 53.7441, Lng: -1.6401, Postcode: "WF17 9TB", Address: "Gelderd Road, Batley"}
	if e != want {
		t.Errorf("got %+v, want %+v", e, want)
	}
	graph := []byte(`<script type="application/ld+json">{"@graph":[{"@type":["Place","MovieTheater"],"name":"Jersey","address":{"addressCountry":"JE"}}]}</script>`)
	if mt, ok := findMovieTheater(graph); !ok || mt.Name != "Jersey" || mt.Address.AddressCountry != "JE" {
		t.Errorf("@graph: %+v %v", mt, ok)
	}
	if _, ok := findMovieTheater([]byte(`<p>Opening soon!</p>`)); ok {
		t.Error("found a theater on a page without one")
	}
}

func TestVue(t *testing.T) {
	for in, want := range map[string]string{
		"Accrington":                         "Vue Accrington",
		"London - Islington (Angel Central)": "Vue Islington Angel Central",
		"London - North Finchley (Great Northern Leisure Park)": "Vue North Finchley",
		"London - Finchley Road (02 Centre)":                    "Vue Finchley Road O2 Centre",
		"Birmingham (Star City)":                                "Vue Birmingham Star City",
		"Vue Basildon":                                          "Vue Basildon",
	} {
		if got := vueName(in); got != want {
			t.Errorf("vueName(%q) = %q, want %q", in, got, want)
		}
	}
	names := vueNames([]vueCinema{{CinemaName: "Leeds", FullName: "Leeds - Kirkstall Road"},
		{CinemaName: "Leeds", FullName: "Leeds - The Light"}, {CinemaName: "York", FullName: "York - Clifton Moor"}})
	if !slices.Equal(names, []string{"Vue Leeds Kirkstall Road", "Vue Leeds The Light", "Vue York"}) {
		t.Errorf("vueNames = %q", names)
	}
	page := []byte(`<a href="https://maps.apple.com/?q=53.754874975913864, -2.369161692538972">x</a>
"cinemaAddress":{"value":"The Viaduct\r\nHyndburn Road\r\nAccrington\r\nBB5 1QF       "},"detailedFacilities":{}`)
	var e chainfile.Entry
	parseVueGettingHere(&e, page)
	if e.Lat != 53.754874975913864 || e.Lng != -2.369161692538972 || e.Postcode != "BB5 1QF" ||
		e.Address != "The Viaduct, Hyndburn Road, Accrington" {
		t.Errorf("getting-here: %+v", e)
	}
}

func TestPicturehouse(t *testing.T) {
	var e chainfile.Entry
	parsePicturehousePage(&e, []byte(`<title>The Cameo | Edinburgh Cinema | Picturehouse</title>
<script>var CINEMA_ID = '022';</script>`))
	parsePicturehouseInfo(&e, []byte(`<a href="https://maps.google.com/maps?q=55.9428,-3.2041&z=15">map</a>
<div class="cinemaAdrass">The Cameo<br>38 Home Street<br>Edinburgh<br>EH3 9LZ</div>`))
	want := chainfile.Entry{Name: "The Cameo", ID: "022", Lat: 55.9428, Lng: -3.2041, Postcode: "EH3 9LZ", Address: "38 Home Street, Edinburgh"}
	if e != want {
		t.Errorf("got %+v, want %+v", e, want)
	}
}

func TestLight(t *testing.T) {
	landing := []byte(`<form><button type="submit" name="go" value="10">
	<strong>Addlestone</strong><svg></svg></button>
	<button type="submit" name="go" value="3"><strong>New Brighton</strong></button></form>`)
	if got := lightPlaces(landing); !slices.Equal(got, []string{"Addlestone", "New Brighton"}) {
		t.Errorf("lightPlaces = %q", got)
	}
	a := lightAddress([]byte(`<div><p class="address">Market Place Shopping Centre,
		Bolton, BL1 2AL</p></div>`))
	if a != "Market Place Shopping Centre, Bolton, BL1 2AL" || findPostcode(a) != "BL1 2AL" ||
		joinAddress(a) != "Market Place Shopping Centre, Bolton" {
		t.Errorf("lightAddress = %q", a)
	}
}

func TestReel(t *testing.T) {
	landing := []byte(`<a href="https://reel-cinemas.co.uk/burnley/">B</a><a href="https://reel-cinemas.co.uk/wp-content/x.png">
	<a href="https://reel-cinemas.co.uk/feed/"></a><a href="https://reel-cinemas.co.uk/burnley/whatson/"></a>
	<a href="https://reel-cinemas.co.uk/cookie-policy/"></a>`)
	if got := reelTowns(landing); !slices.Equal(got, []string{"burnley", "cookie-policy"}) {
		t.Errorf("reelTowns = %q", got)
	}
	var e chainfile.Entry
	if !parseReelFooter(&e, []byte(`<p>&copy; 2026 REEL Cinema Fareham Hampshire, 3 Vannes Parade, Market Quay, Fareham, Hampshire, PO16 0BX</p>`)) {
		t.Fatal("no footer")
	}
	if got := reelPlace("Bishop Auckland", []string{"Newgate Street", "Bishop Auckland", "Durham"}); got != "Bishop Auckland" {
		t.Errorf("reelPlace = %q", got)
	}
	if e.Name != "Reel Fareham" || e.Postcode != "PO16 0BX" || e.Address != "3 Vannes Parade, Market Quay, Fareham, Hampshire" {
		t.Errorf("footer: %+v", e)
	}
}

func ent(name, id, slug string) chainfile.Entry {
	return chainfile.Entry{Name: name, Chain: "Odeon", ID: id, Slug: slug, Lat: 53, Lng: -2, Region: "north-west"}
}

func TestMergeChain(t *testing.T) {
	prev := []chainfile.Entry{ent("Odeon Liverpool ONE", "560", "liverpool-one"), ent("Odeon Gone", "1", "gone"),
		ent("Odeon Kept Coords", "2", "kc")}
	noCoords := ent("Odeon Kept Coords (new name)", "2", "kc")
	noCoords.Lat, noCoords.Region = 0, ""
	cur := []chainfile.Entry{ent("Odeon Liverpool One", "560", "liverpool-one"), ent("Odeon New", "3", "new"),
		noCoords, ent("Odeon Missing", "4", "m"), ent("Odeon Dup", "3", "new")}
	cur[3].Region = "dublin"
	out, sum := mergeChain("Odeon", prev, cur, nil)
	var names []string
	for _, e := range out {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"Odeon Liverpool ONE", "Odeon New", "Odeon Kept Coords"}) {
		t.Errorf("names = %q", names)
	}
	if out[2].Lat != 53 || out[2].Region != "north-west" {
		t.Errorf("coords not borrowed: %+v", out[2])
	}
	if !slices.Equal(sum.Added, []string{"Odeon New"}) || !slices.Equal(sum.Removed, []string{"Odeon Gone"}) ||
		len(sum.Skipped) != 2 || sum.Count != 3 || sum.Kept != "" {
		t.Errorf("summary = %+v", sum)
	}

	// Slug identifies entries without an ID.
	lp := []chainfile.Entry{{Name: "The Light New Brighton", Chain: "The Light", Slug: "newbrighton", Lat: 53, Lng: -3, Region: "north-west"}}
	lc := []chainfile.Entry{{Name: "The Light Newbrighton", Chain: "The Light", Slug: "newbrighton", Lat: 53.4, Lng: -3, Region: "north-west"}}
	if out, _ := mergeChain("The Light", lp, lc, nil); out[0].Name != "The Light New Brighton" || out[0].Lat != 53.4 {
		t.Errorf("slug merge: %+v", out)
	}

	// A failed or shrunken discovery keeps the previous entries.
	if out, sum := mergeChain("Odeon", prev, nil, errors.New("HTTP 403")); len(out) != 3 || sum.Kept != "HTTP 403" {
		t.Errorf("failure: %d entries, %+v", len(out), sum)
	}
	if out, sum := mergeChain("Odeon", prev, cur[:1], nil); len(out) != 3 || sum.Kept == "" {
		t.Errorf("shrunk: %d entries, %+v", len(out), sum)
	}
	if out, sum := mergeChain("Odeon", nil, cur[:2], nil); len(out) != 2 || len(sum.Added) != 2 {
		t.Errorf("first run: %d entries, %+v", len(out), sum)
	}
}

func TestUniqueNames(t *testing.T) {
	if err := uniqueNames([]chainfile.Entry{ent("A", "1", ""), ent("B", "2", "")}); err != nil {
		t.Error(err)
	}
	if err := uniqueNames([]chainfile.Entry{ent("A", "1", ""), ent("A", "2", "")}); err == nil {
		t.Error("duplicate not caught")
	}
}

func TestSelected(t *testing.T) {
	if !selected("The Light", []string{"light"}) || selected("Vue", []string{"odeon", " curzon"}) || !selected("Vue", nil) {
		t.Error("selected")
	}
}
