// Package registry is the list of cinemas the site covers.
package registry

import (
	"strings"

	"fazon.nexus/cinema/internal/scrape"
	"fazon.nexus/cinema/internal/scrape/chains"
	"fazon.nexus/cinema/internal/scrape/ticketing"
	"fazon.nexus/cinema/internal/scrape/venues"
)

const (
	cw = "https://www.cineworld.co.uk"
	ev = "https://www.everymancinema.com"
	sc = "https://www.showcasecinemas.co.uk"
)

// Cinemas is every cinema, in display order. Relay = hard-blocked (403) from the
// Hetzner IP, so scraped from a residential connection by cinema-relay.
var Cinemas = build()

func build() []scrape.Cinema {
	var cs []scrape.Cinema
	add := func(name, chain, url string, relay bool, f scrape.Fetcher) {
		cs = append(cs, scrape.Cinema{Name: name, Chain: chain, URL: url, Relay: relay, Fetch: f})
	}
	for _, o := range [][3]string{
		{"Manchester Great Northern", "manchester-great-northern", "856"}, {"Trafford Centre", "manchester-trafford-centre", "955"},
		{"Oldham", "oldham", "851"}, {"Rochdale", "rochdale", "843"}, {"Sheffield", "sheffield", "209"},
		{"Liverpool ONE", "liverpool-one", "560"},
	} {
		add("Odeon "+o[0], "Odeon", "https://www.odeon.co.uk/cinemas/"+o[1]+"/", true, chains.Odeon(o[1], o[2]))
	}
	for _, l := range [][2]string{{"Bolton", "bolton"}, {"Stockport", "stockport"}, {"Sheffield", "sheffield"},
		{"Huddersfield", "huddersfield"}, {"New Brighton", "newbrighton"}, {"Bradford", "bradford"}} {
		add("The Light "+l[0], "The Light", "https://"+l[1]+".thelight.co.uk/", false, chains.Light(l[1]))
	}
	for _, c := range [][2]string{{"Bradford", "x06x2-cineworld-cinema-bradford"}, {"Bolton", "x079p-cineworld-cinema-bolton"},
		{"Didsbury", "x079r-cineworld-cinema-didsbury"}, {"Ashton", "x06wz-cineworld-cinema-ashton-under-lyne"}} {
		add("Cineworld "+c[0], "Cineworld", cw+"/cinemas/"+c[1]+"/", true, chains.Webedia(cw, strings.ToUpper(c[1][:5])))
	}
	for _, p := range [][3]string{{"Picturehouse at FACT Liverpool", "013", "picturehouse-at-fact"},
		{"City Screen Picturehouse York", "018", "city-screen-picturehouse"}, {"Chester Picturehouse", "032", "chester-picturehouse"}} {
		add(p[0], "Picturehouse", "https://www.picturehouses.com/cinema/"+p[2], false, chains.Picturehouse(p[1], p[2]))
	}
	for _, v := range [][3]string{{"Manchester Printworks", "manchester-printworks", "10091"}, {"Bury", "bury-the-rock", "10073"},
		{"Bolton", "bolton", "10040"}, {"MediaCity", "manchester", "10057"}, {"Blackburn", "blackburn", "10007"}} {
		add("Vue "+v[0], "Vue", "https://www.myvue.com/cinema/"+v[1]+"/whats-on", true, chains.Vue(v[2]))
	}
	add("Curzon Knutsford", "Curzon", "https://www.curzon.com/venues/knutsford/", true,
		chains.Vista("https://www.curzon.com/venues/knutsford/", "https://digital-api.curzon.com", "KNU1"))
	// Curzon Sheffield closed permanently on 29 June 2026.
	for _, e := range [][2]string{{"Manchester St Johns", "x11np-everyman-manchester-st-johns"},
		{"Altrincham", "x11dp-everyman-altrincham"}, {"Clitheroe", "x11nr-everyman-clitheroe"},
		{"Liverpool", "x11i7-everyman-liverpool"}, {"York", "x06tj-everyman-york"}, {"Leeds", "x0qm5-everyman-leeds"}} {
		add("Everyman "+e[0], "Everyman", ev+"/venues-list/"+e[1]+"/", false, chains.Webedia(ev, strings.ToUpper(e[1][:5])))
	}
	add("Showcase Leeds", "Showcase", sc+"/cinemas/x06jk-showcase-cinema-de-lux-leeds/", false, chains.Webedia(sc, "X06JK"))
	add("Showcase Liverpool", "Showcase", sc+"/cinemas/x06ju-showcase-cinema-de-lux-liverpool/", false, chains.Webedia(sc, "X06JU"))
	for _, t := range []string{"rochdale", "burnley", "blackburn"} {
		add("Reel "+strings.ToUpper(t[:1])+t[1:], "Reel", "https://reel-cinemas.co.uk/"+t+"/whatson/", false, chains.Reel(t))
	}
	for _, i := range [][2]string{{"Savoy Cinema Heaton Moor", "https://www.savoycinemaheatonmoor.com"},
		{"Rex Cinema Wilmslow", "https://www.rexcinemawilmslow.com"}, {"Northern Light Cinema Sale", "https://www.tnlcinemasale.co.uk"}} {
		add(i[0], "Independent", i[1], false, ticketing.Indy(i[1]))
	}
	ind := func(name, url string, f scrape.Fetcher) { add(name, "Independent", url, false, f) }
	ind("Leigh Film Factory", "https://leighfilmfactory.com", ticketing.LeighFilmFactory)
	ind("Southport Bijou Cinema", "https://southportbijoucinema.co.uk", ticketing.SouthportBijou)
	ind("Partington Theatre Glossop", "https://peakfilmsociety.org/whats-on/", ticketing.PartingtonGlossop)
	ind("Altrincham Little Theatre", "https://mandsfilmsociety.org.uk", ticketing.AltrinchamLittleTheatre)
	ind("Stockport Garrick Theatre", "https://www.stockportgarrick.co.uk", ticketing.StockportGarrick)
	ind("Cultplex", "https://cultplex.co.uk", venues.Cultplex)
	ind("Hebden Bridge Picture House", "https://hebdenbridgepicturehouse.co.uk", venues.HebdenBridge)
	ind("Liverpool Plaza Community Cinema", "https://plazacinema.org.uk", venues.PlazaCrosby)
	add("Plaza Cinema Skipton", "Independent", "https://plazaskipton.co.uk", true, venues.PlazaSkipton)
	ind("Penistone Paramount", "https://penistoneparamount.co.uk", venues.Penistone)
	ind("Pictureville Bradford", "https://www.scienceandmediamuseum.org.uk/cinema/listings", venues.Pictureville)
	ind("Stockport Plaza", "https://stockportplaza.co.uk/whats-on/type/film/", venues.StockportPlaza)
	ind("Tideswell Cinema", "https://tideswellcinema.com/all-screenings/", venues.Tideswell)
	ind("HOME Manchester", "https://www.homemcr.org/what-s-on-cinema-5pnz", ticketing.HomeManchester)
	ind("Showroom Sheffield", "https://showroomworkstation.org.uk", ticketing.ShowroomSheffield)
	ind("Storyhouse Chester", "https://www.storyhouse.com", ticketing.StoryhouseChester)
	ind("Altrincham Garrick Playhouse", "https://www.altrinchamgarrick.co.uk", ticketing.AltrinchamGarrick)
	ind("Buxton Opera House", "https://buxtonoperahouse.org.uk", ticketing.BuxtonOperaHouse)
	// Not scraped: King Street Townhouse (private-hire only), Square Chapel Halifax (closed 2025).
	return cs
}

// Select filters cinemas: only (comma-separated substrings of "name chain", case-insensitive;
// empty = all) and relay (nil = either, else must match the Relay flag).
func Select(only string, relay *bool) []scrape.Cinema {
	var keys []string
	for _, k := range strings.Split(only, ",") {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			keys = append(keys, k)
		}
	}
	var out []scrape.Cinema
	for _, c := range Cinemas {
		if relay != nil && c.Relay != *relay {
			continue
		}
		hay := strings.ToLower(c.Name + " " + c.Chain)
		ok := len(keys) == 0
		for _, k := range keys {
			ok = ok || strings.Contains(hay, k)
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

// ByName looks a cinema up by exact name.
func ByName(name string) (scrape.Cinema, bool) {
	for _, c := range Cinemas {
		if c.Name == name {
			return c, true
		}
	}
	return scrape.Cinema{}, false
}
