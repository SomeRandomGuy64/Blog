package listings

import "math"

// Place is a cinema's location: chain APIs where they publish coordinates, OpenStreetMap for
// a few small venues, else the venue's postcode centroid (postcodes.io).
type Place struct {
	Lat, Lng          float64
	Postcode, Address string
}

// Places locates every cinema in the registry (keyed by name).
var Places = map[string]Place{
	"Odeon Manchester Great Northern":  {53.4773, -2.249, "M3 4EN", "Great Northern, 235 Deansgate, Manchester"},
	"Odeon Trafford Centre":            {53.4658, -2.348, "M17 8DF", "The Trafford Centre, Manchester"},
	"Odeon Oldham":                     {53.5418, -2.1111, "OL1 1RA", "Old Town Hall, Parliament Square, Oldham"},
	"Odeon Rochdale":                   {53.603, -2.1644, "OL11 1RY", "Sandbrook Park, Sandbrook Way, Rochdale"},
	"Odeon Sheffield":                  {53.3807, -1.4651, "S1 1DL", "Arundel Gate, Sheffield"},
	"Odeon Liverpool ONE":              {53.4043, -2.9868, "L1 8JF", "Liverpool ONE, Paradise Street, Liverpool"},
	"The Light Bolton":                 {53.58131, -2.42966, "BL1 2AL", "Market Place Shopping Centre, Bolton"},
	"The Light Stockport":              {53.41181, -2.16209, "SK1 1SA", "Redrock, Bridgefield Street, Stockport"},
	"The Light Sheffield":              {53.37635, -1.47303, "S1 4PF", "The Moor, Sheffield"},
	"The Light Huddersfield":           {53.64593, -1.77911, "HD1 2QB", "Kingsgate Shopping Centre, King Street, Huddersfield"},
	"The Light New Brighton":           {53.4396, -3.05332, "CH45 2HZ", "Marine Point, New Brighton, Wallasey"},
	"The Light Bradford":               {53.79392, -1.74867, "BD1 1JR", "The Broadway, Hall Ings, Bradford"},
	"Cineworld Bradford":               {53.7912, -1.7472, "BD1 5LD", "Bradford Leisure Exchange, Bradford"},
	"Cineworld Bolton":                 {53.5975, -2.4225, "BL1 8TS", "The Valley, Bolton"},
	"Cineworld Didsbury":               {53.4097, -2.2193, "M20 5PG", "Parrs Wood Entertainment Centre, Didsbury, Manchester"},
	"Cineworld Ashton":                 {53.4892, -2.1102, "OL7 0PG", "Ashton Leisure Park, Ashton-under-Lyne"},
	"Picturehouse at FACT Liverpool":   {53.4022, -2.9776, "L1 4DQ", "FACT, 88 Wood Street, Liverpool"},
	"City Screen Picturehouse York":    {53.9591, -1.0848, "YO1 9QL", "13-17 Coney Street, York"},
	"Chester Picturehouse":             {53.19254, -2.8937, "CH1 2AR", "Unit 10, Hunter Street, Chester"},
	"Vue Manchester Printworks":        {53.48519, -2.24078, "M4 2BS", "The Printworks, 27 Withy Grove, Manchester"},
	"Vue Bury":                         {53.59423, -2.29161, "BL9 0JX", "The Rock, 9 Rock Place, Bury"},
	"Vue Bolton":                       {53.58337, -2.53572, "BL6 6JA", "Middlebrook Leisure Park, The Linkway, Horwich, Bolton"},
	"Vue MediaCity":                    {53.47091, -2.29306, "M50 3AH", "Lowry Outlet Mall, The Quays, Salford (Manchester Quayside)"},
	"Vue Blackburn":                    {53.74415, -2.48047, "BB1 1DG", "Peel Leisure and Retail Park, Lower Audley Street, Blackburn"},
	"Curzon Knutsford":                 {53.302, -2.3727, "WA16 0PE", "Toft Road, Knutsford"},
	"Everyman Manchester St Johns":     {53.4788, -2.2528, "M3 4AS", "ABC Building, Quay Street, Manchester"},
	"Everyman Altrincham":              {53.3864, -2.3511, "WA14 1RF", "74-78 George Street, Altrincham"},
	"Everyman Clitheroe":               {53.868, -2.3945, "BB7 1EB", "Greenacre Street, Clitheroe"},
	"Everyman Liverpool":               {53.4068, -2.9851, "L1 6DA", "35 Whitechapel, Liverpool"},
	"Everyman York":                    {53.9547, -1.0929, "YO24 1AJ", "Blossom Street, York"},
	"Everyman Leeds":                   {53.7971, -1.5452, "LS1 5AY", "Level 4, Trinity Leeds, Albion Street, Leeds"},
	"Showcase Leeds":                   {53.7441, -1.6401, "WF17 9TB", "Gelderd Road, Birstall, Batley"},
	"Showcase Liverpool":               {53.4571, -2.9137, "L11 9YJ", "340 East Lancashire Road, Norris Green, Liverpool"},
	"Reel Rochdale":                    {53.61773, -2.15508, "OL16 1BE", "Unit 22, Rochdale Riverside, Riverside Walk, Rochdale"},
	"Reel Burnley":                     {53.79175, -2.24563, "BB11 1FB", "Unit 1, Pioneer Place, Curzon Street, Burnley"},
	"Reel Blackburn":                   {53.74846, -2.48766, "BB2 1LN", "Nab Lane, Blackburn"},
	"Savoy Cinema Heaton Moor":         {53.41768, -2.18903, "SK4 4HY", "105 Heaton Moor Road, Heaton Moor, Stockport"},
	"Rex Cinema Wilmslow":              {53.32572, -2.2321, "SK9 1HY", "21 Alderley Road, Wilmslow"},
	"Northern Light Cinema Sale":       {53.42383, -2.3225, "M33 7WZ", "15 Stanley Square, Sale"},
	"Leigh Film Factory":               {53.4935, -2.49556, "WN7 2LA", "Room 4.17, Leigh Spinners Mill, Park Lane, Leigh"},
	"Southport Bijou Cinema":           {53.64871, -3.0028, "PR9 0US", "8 Post Office Avenue, Southport"},
	"Partington Theatre Glossop":       {53.44372, -1.94948, "SK13 8BW", "Partington Players Theatre, Henry Street, Glossop"},
	"Altrincham Little Theatre":        {53.3834, -2.35124, "WA14 2ED", "17 Oxford Road, Altrincham"},
	"Stockport Garrick Theatre":        {53.4083, -2.16176, "SK3 0EJ", "Exchange Street, Wellington Road South, Stockport"},
	"Cultplex":                         {53.49161, -2.23792, "M4 4HF", "50 Red Bank, Cheetham Hill, Manchester"},
	"Hebden Bridge Picture House":      {53.74068, -2.0115, "HX7 8AD", "New Road, Hebden Bridge"},
	"Liverpool Plaza Community Cinema": {53.47762, -3.02334, "L22 0LD", "13 Crosby Road North, Waterloo, Liverpool"},
	"Plaza Cinema Skipton":             {53.95856, -2.01768, "BD23 2PB", "Sackville Street, Skipton"},
	"Penistone Paramount":              {53.5258, -1.6282, "S36 6DY", "Shrewsbury Road, Penistone"},
	"Pictureville Bradford":            {53.79068, -1.75611, "BD1 1NQ", "National Science and Media Museum, Little Horton Lane, Bradford"},
	"Stockport Plaza":                  {53.40924, -2.16137, "SK1 1SP", "Mersey Square, Stockport"},
	"Tideswell Cinema":                 {53.27867, -1.77073, "SK17 8PD", "Tideswell Community Hall, Tideswell, Buxton"},
	"HOME Manchester":                  {53.47293, -2.2466, "M15 4FN", "2 Tony Wilson Place, First Street, Manchester"},
	"Showroom Sheffield":               {53.37706, -1.46566, "S1 2BX", "15 Paternoster Row, Sheffield"},
	"Storyhouse Chester":               {53.19228, -2.89362, "CH1 2AR", "Hunter Street, Chester"},
	"Altrincham Garrick Playhouse":     {53.39429, -2.35037, "WA14 1HZ", "Barrington Road, Altrincham"},
	"Buxton Opera House":               {53.25857, -1.91579, "SK17 6AZ", "5 The Square, Buxton"},
}

// Estimate is the straight-line travel formula, mirrored in the page's JS:
//
//	km < WalkKmMax ? km*WalkMinPerKm : TransitBaseMin + km*TransitMinPerKm
//
// walk: 13 min/km x 1.3 street detour. transit: least-squares fit to ~35 hand-checked
// door-to-door weekday trips. Typical error ~10 min; it underestimates rural Peak trips and
// slow orbital buses, and overestimates fast intercity rail. WalkKmMax is where the lines meet.
type EstimateParams struct {
	WalkKmMax       float64 `json:"walk_km_max"`
	WalkMinPerKm    float64 `json:"walk_min_per_km"`
	TransitBaseMin  float64 `json:"transit_base_min"`
	TransitMinPerKm float64 `json:"transit_min_per_km"`
}

var Estimate = EstimateParams{WalkKmMax: 2.2, WalkMinPerKm: 17.0, TransitBaseMin: 35.0, TransitMinPerKm: 1.25}

// HaversineKm is the great-circle distance in km.
func HaversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	la1, lo1, la2, lo2 := lat1*rad, lng1*rad, lat2*rad, lng2*rad
	h := math.Pow(math.Sin((la2-la1)/2), 2) + math.Cos(la1)*math.Cos(la2)*math.Pow(math.Sin((lo2-lo1)/2), 2)
	return 12742.0 * math.Asin(math.Sqrt(h))
}

// EstimateMinutes converts a crow-fly distance to door-to-door minutes (Python round(): half-even).
func EstimateMinutes(km float64) int {
	if km < Estimate.WalkKmMax {
		return int(math.RoundToEven(km * Estimate.WalkMinPerKm))
	}
	return int(math.RoundToEven(Estimate.TransitBaseMin + km*Estimate.TransitMinPerKm))
}
