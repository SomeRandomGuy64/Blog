// Package registry is the list of cinemas the site covers.
package registry

import (
	_ "embed"
	"slices"
	"strings"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/scrape"
	"fazon.nexus/cinema/internal/scrape/chains"
	"fazon.nexus/cinema/internal/scrape/ticketing"
	"fazon.nexus/cinema/internal/scrape/venues"
)

// webedia is each Webedia chain's site (its API lives under the same host).
var webedia = map[string]string{
	"Cineworld": "https://www.cineworld.co.uk",
	"Everyman":  "https://www.everymancinema.com",
	"Showcase":  "https://www.showcasecinemas.co.uk",
}

// relayChains block the server's datacentre IP, so their cinemas are scraped by cinema-relay.
var relayChains = map[string]bool{"Odeon": true, "Cineworld": true, "Vue": true, "Curzon": true}

// relayCinemas are the independents that block it too (taposapp sites).
var relayCinemas = map[string]bool{"Plaza Cinema Skipton": true, "Picture House Keighley": true, "Rex Cinema Elland": true}

//go:embed chains.json
var chainsJSON []byte

// Cinemas is every cinema, in display order: the big chains (chains.json, from `cinema discover`)
// then the hand-written independents. Relay = hard-blocked (403) from the Hetzner IP, so scraped
// from a residential connection by cinema-relay.
var Cinemas = build()

func build() []scrape.Cinema {
	f, err := chainfile.Parse(chainsJSON)
	if err != nil {
		panic("registry: chains.json: " + err.Error())
	}
	var cs []scrape.Cinema
	for _, e := range f.Cinemas {
		cs = append(cs, scrape.Cinema{Name: e.Name, Chain: e.Chain, URL: e.URL, Relay: relayChains[e.Chain],
			Fetch: chainFetcher(e), Lat: e.Lat, Lng: e.Lng, Region: e.Region})
	}
	add := func(name, chain, url string, relay bool, f scrape.Fetcher) {
		p := Places[name] // registry_test guarantees every independent is located
		cs = append(cs, scrape.Cinema{Name: name, Chain: chain, URL: url, Relay: relay, Fetch: f,
			Lat: p.Lat, Lng: p.Lng, Region: p.Region})
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
	// taposapp sites block the server's IP too, so they are relay cinemas.
	for _, i := range [][2]string{{"Plaza Cinema Skipton", "https://plazaskipton.co.uk"},
		{"Picture House Keighley", "https://picturehousekeighley.co.uk"}, {"Rex Cinema Elland", "https://rexelland.co.uk"}} {
		pages := []string{} // Keighley and Elland's event-cinema pages list films but no showtimes: skip the home request
		if i[0] != "Plaza Cinema Skipton" {
			pages = []string{"", "coming-soon"}
		}
		add(i[0], "Independent", i[1], relayCinemas[i[0]], venues.Taposapp(i[1]+"/", pages...))
	}
	ind("Penistone Paramount", "https://penistoneparamount.co.uk", venues.Penistone)
	ind("Pictureville Bradford", "https://www.scienceandmediamuseum.org.uk/cinema/listings", venues.Pictureville)
	ind("Stockport Plaza", "https://stockportplaza.co.uk/whats-on/type/film/", venues.StockportPlaza)
	ind("Tideswell Cinema", "https://tideswellcinema.com/all-screenings/", venues.Tideswell)
	ind("HOME Manchester", "https://www.homemcr.org/what-s-on-cinema-5pnz", ticketing.HomeManchester)
	ind("Showroom Sheffield", "https://showroomworkstation.org.uk", ticketing.ShowroomSheffield)
	ind("Storyhouse Chester", "https://www.storyhouse.com", ticketing.StoryhouseChester)
	ind("Altrincham Garrick Playhouse", "https://www.altrinchamgarrick.co.uk", ticketing.AltrinchamGarrick)
	ind("Buxton Opera House", "https://buxtonoperahouse.org.uk", ticketing.BuxtonOperaHouse)
	ind("The Dukes Lancaster", "https://dukeslancaster.org", ticketing.DukesLancaster)
	ind("Lockworks Cinema Wolverhampton", "https://lockworkscinema.co.uk", venues.AdmitOne("https://lockworkscinema.co.uk"))
	ind("Prince Charles Cinema", "https://princecharlescinema.com/whats-on/", venues.Jacro("https://princecharlescinema.com/whats-on/"))
	ind("Cottage Road Cinema", "https://cottageroad.co.uk", venues.Jacro("https://cottageroad.co.uk/"))
	ind("Cinemac Macclesfield", "https://cinemac.org.uk", venues.Jacro("https://cinemac.org.uk/"))
	// Arc and Parkway are small chains of their own (no chains.json: no discovery for them).
	for _, site := range []string{"Beeston", "Blackpool", "Daventry", "Great Yarmouth", "Hucknall", "Preston", "Rotherham", "Peterhead"} {
		u := "https://" + strings.ToLower(strings.ReplaceAll(site, " ", "")) + ".arccinema.co.uk"
		add("Arc "+site, "Arc", u, false, venues.AdmitOne(u))
	}
	add("Parkway Barnsley", "Parkway", "https://barnsley.parkwaycinemas.co.uk", false, venues.AdmitOne("https://barnsley.parkwaycinemas.co.uk"))
	for _, site := range []string{"Beverley", "Cleethorpes", "Louth", "Workington"} {
		u := "https://" + strings.ToLower(site) + ".parkwaycinemas.co.uk"
		add("Parkway "+site, "Parkway", u, false, venues.Glitterfish(u))
	}
	// Not scraped: King Street Townhouse (private-hire only), Square Chapel Halifax (closed 2025).
	return cs
}

// chainFetcher builds a discovered cinema's fetcher from its chain and ids (see chainfile.Entry).
func chainFetcher(e chainfile.Entry) scrape.Fetcher {
	switch e.Chain {
	case "Odeon":
		return chains.Odeon(e.Slug, e.ID)
	case "Curzon":
		return chains.Vista(e.URL, "https://digital-api.curzon.com", e.ID)
	case "Cineworld", "Everyman", "Showcase":
		return chains.Webedia(webedia[e.Chain], e.ID)
	case "Vue":
		return chains.Vue(e.ID)
	case "Picturehouse":
		return chains.Picturehouse(e.ID, e.Slug)
	case "The Light":
		return chains.Light(e.Slug)
	case "Reel":
		return chains.Reel(e.Slug)
	}
	panic("registry: chains.json: unknown chain " + e.Chain)
}

// InRegions returns the cinemas in any of the given regions, in display order.
func InRegions(keys []string) []scrape.Cinema {
	var out []scrape.Cinema
	for _, c := range Cinemas {
		if slices.Contains(keys, c.Region) {
			out = append(out, c)
		}
	}
	return out
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
