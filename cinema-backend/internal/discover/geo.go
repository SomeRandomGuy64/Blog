package discover

import (
	"math"
	"net/url"
	"slices"
	"strings"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/region"
	"fazon.nexus/cinema/internal/scrape"
)

const postcodesIO = "https://api.postcodes.io/postcodes"

// pcResult is the part of a postcodes.io postcode we use.
type pcResult struct {
	Postcode  string  `json:"postcode"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Region    string  `json:"region"`  // England only, else null
	Country   string  `json:"country"` // England, Scotland, Wales, Northern Ireland
	Distance  float64 `json:"distance"`
}

// lookupPostcodes bulk-geocodes postcodes (nil for unknown ones).
func lookupPostcodes(s *scrape.Session, pcs []string) (map[string]*pcResult, error) {
	out := map[string]*pcResult{}
	for chunk := range slices.Chunk(pcs, 100) {
		var j struct {
			Result []struct {
				Query  string    `json:"query"`
				Result *pcResult `json:"result"`
			} `json:"result"`
		}
		if err := s.PostJSON(postcodesIO, &j, scrape.JSONBody(map[string]any{"postcodes": chunk})); err != nil {
			return nil, err
		}
		for _, r := range j.Result {
			out[r.Query] = r.Result
		}
	}
	return out, nil
}

// nearestPostcodes bulk reverse-geocodes points (nil where there is no postcode within radius
// metres, i.e. outside the UK).
func nearestPostcodes(s *scrape.Session, pts [][2]float64, radius int) ([]*pcResult, error) {
	var out []*pcResult
	for chunk := range slices.Chunk(pts, 100) {
		geo := make([]map[string]any, len(chunk))
		for i, p := range chunk {
			geo[i] = map[string]any{"latitude": p[0], "longitude": p[1], "limit": 1, "radius": radius}
		}
		var j struct {
			Result []struct {
				Result []pcResult `json:"result"`
			} `json:"result"`
		}
		if err := s.PostJSON(postcodesIO, &j, scrape.JSONBody(map[string]any{"geolocations": geo})); err != nil {
			return nil, err
		}
		for _, r := range j.Result {
			if len(r.Result) == 0 {
				out = append(out, nil)
			} else {
				out = append(out, &r.Result[0])
			}
		}
	}
	return out, nil
}

// locate fills Region (and coordinates from the postcode when the chain gave none, or ones
// more than 5 km from it) from postcodes.io: the cinema's own postcode first, else the nearest
// postcode to its coordinates.
// Cinemas with coordinates but no postcode within 10 km are outside the UK and dropped.
func locate(s *scrape.Session, es []chainfile.Entry, log func(string, ...any)) ([]chainfile.Entry, error) {
	var pcs []string
	for _, e := range es {
		if e.Postcode != "" && !slices.Contains(pcs, e.Postcode) {
			pcs = append(pcs, e.Postcode)
		}
	}
	byPC, err := lookupPostcodes(s, pcs)
	if err != nil {
		return nil, err
	}
	var todo []int // need a reverse lookup
	drop, approx := map[int]bool{}, map[int]bool{}
	for i := range es {
		e := &es[i]
		if r := byPC[e.Postcode]; r != nil {
			if !ukCountry(r.Country) {
				log("dropping %s: postcode %s is in %s", e.Name, r.Postcode, r.Country)
				drop[i] = true
				continue
			}
			e.Postcode, e.Region = r.Postcode, region.FromPostcodesIO(r.Region, r.Country)
			if e.Lat == 0 || e.Lng == 0 {
				e.Lat, e.Lng = r.Latitude, r.Longitude
			} else if d := km(e.Lat, e.Lng, r.Latitude, r.Longitude); r.Latitude != 0 && d > 5 { // a misplaced pin
				log("%s: coordinates are %.0f km from postcode %s, using the postcode's", e.Name, d, r.Postcode)
				e.Lat, e.Lng = r.Latitude, r.Longitude
			}
			continue
		}
		if e.Postcode != "" {
			if e.Lat == 0 || e.Lng == 0 {
				if lat, lng, near, ok := approxPostcode(s, e.Postcode); ok {
					log("%s: postcode %q not found, located by %s", e.Name, e.Postcode, near)
					e.Lat, e.Lng = lat, lng
					approx[i] = true
				}
			} else {
				log("%s: postcode %q not found", e.Name, e.Postcode)
			}
			e.Postcode = ""
		}
		if e.Lat != 0 && e.Lng != 0 {
			todo = append(todo, i)
		}
	}
	for _, radius := range []int{2000, 10000} {
		if len(todo) == 0 {
			break
		}
		pts := make([][2]float64, len(todo))
		for k, i := range todo {
			pts[k] = [2]float64{es[i].Lat, es[i].Lng}
		}
		rs, err := nearestPostcodes(s, pts, radius)
		if err != nil {
			return nil, err
		}
		var miss []int
		for k, i := range todo {
			r := rs[k]
			if r == nil {
				miss = append(miss, i)
				continue
			}
			e := &es[i]
			e.Region = region.FromPostcodesIO(r.Region, r.Country)
			if e.Postcode == "" && r.Distance <= 200 && !approx[i] {
				e.Postcode = r.Postcode
			}
		}
		todo = miss
	}
	for _, i := range todo {
		log("dropping %s (%.4f, %.4f): no UK postcode within 10 km", es[i].Name, es[i].Lat, es[i].Lng)
		drop[i] = true
	}
	var out []chainfile.Entry
	for i, e := range es {
		if !drop[i] {
			out = append(out, e)
		}
	}
	return out, nil
}

// approxPostcode locates a postcode postcodes.io doesn't know (a typo or a new one): as a
// terminated postcode, else the mean of its unit prefix ("RH1 1R"), sector ("RH1 1") or the
// outcode's centroid. near says which.
func approxPostcode(s *scrape.Session, pc string) (lat, lng float64, near string, ok bool) {
	var t struct {
		Result struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"result"`
	}
	if s.GetJSON("https://api.postcodes.io/terminated_postcodes/"+url.PathEscape(pc), &t) == nil && t.Result.Latitude != 0 {
		return t.Result.Latitude, t.Result.Longitude, "terminated postcode", true
	}
	out, in, _ := strings.Cut(pc, " ")
	for _, prefix := range []string{out + " " + in[:min(2, len(in))], out + " " + in[:min(1, len(in))]} {
		var q struct {
			Result []pcResult `json:"result"`
		}
		if s.GetJSON(postcodesIO, &q, scrape.Query(url.Values{"q": {prefix}, "limit": {"100"}})) != nil || len(q.Result) == 0 {
			continue
		}
		n := 0.0
		for _, r := range q.Result {
			if r.Latitude != 0 {
				lat, lng, n = lat+r.Latitude, lng+r.Longitude, n+1
			}
		}
		if n > 0 {
			return lat / n, lng / n, "postcodes " + prefix + "*", true
		}
	}
	if s.GetJSON("https://api.postcodes.io/outcodes/"+url.PathEscape(out), &t) == nil && t.Result.Latitude != 0 {
		return t.Result.Latitude, t.Result.Longitude, "outcode " + out, true
	}
	return 0, 0, "", false
}

func ukCountry(c string) bool {
	return c == "England" || c == "Scotland" || c == "Wales" || c == "Northern Ireland"
}

// km is the great-circle distance between two points.
func km(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371
	rad := math.Pi / 180
	dlat, dlng := (lat2-lat1)*rad, (lng2-lng1)*rad
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dlng/2)*math.Sin(dlng/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}
