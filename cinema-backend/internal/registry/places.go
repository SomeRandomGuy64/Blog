package registry

// Place is an independent cinema's location: OpenStreetMap for a few small venues, else the venue's
// postcode centroid (postcodes.io). Region is the postcodes.io region of its postcode.
type Place struct {
	Lat, Lng          float64
	Postcode, Address string
	Region            string // region.All key
}

// Places locates the independents (keyed by name); the chains come located from chains.json.
var Places = map[string]Place{
	"Savoy Cinema Heaton Moor":         {53.41768, -2.18903, "SK4 4HY", "105 Heaton Moor Road, Heaton Moor, Stockport", "north-west"},
	"Rex Cinema Wilmslow":              {53.32572, -2.2321, "SK9 1HY", "21 Alderley Road, Wilmslow", "north-west"},
	"Northern Light Cinema Sale":       {53.42383, -2.3225, "M33 7WZ", "15 Stanley Square, Sale", "north-west"},
	"Leigh Film Factory":               {53.4935, -2.49556, "WN7 2LA", "Room 4.17, Leigh Spinners Mill, Park Lane, Leigh", "north-west"},
	"Southport Bijou Cinema":           {53.64871, -3.0028, "PR9 0US", "8 Post Office Avenue, Southport", "north-west"},
	"Partington Theatre Glossop":       {53.44372, -1.94948, "SK13 8BW", "Partington Players Theatre, Henry Street, Glossop", "east-midlands"},
	"Altrincham Little Theatre":        {53.3834, -2.35124, "WA14 2ED", "17 Oxford Road, Altrincham", "north-west"},
	"Stockport Garrick Theatre":        {53.4083, -2.16176, "SK3 0EJ", "Exchange Street, Wellington Road South, Stockport", "north-west"},
	"Cultplex":                         {53.49161, -2.23792, "M4 4HF", "50 Red Bank, Cheetham Hill, Manchester", "north-west"},
	"Hebden Bridge Picture House":      {53.74068, -2.0115, "HX7 8AD", "New Road, Hebden Bridge", "yorkshire"},
	"Liverpool Plaza Community Cinema": {53.47762, -3.02334, "L22 0LD", "13 Crosby Road North, Waterloo, Liverpool", "north-west"},
	"Plaza Cinema Skipton":             {53.95856, -2.01768, "BD23 2PB", "Sackville Street, Skipton", "yorkshire"},
	"Penistone Paramount":              {53.5258, -1.6282, "S36 6DY", "Shrewsbury Road, Penistone", "yorkshire"},
	"Pictureville Bradford":            {53.79068, -1.75611, "BD1 1NQ", "National Science and Media Museum, Little Horton Lane, Bradford", "yorkshire"},
	"Stockport Plaza":                  {53.40924, -2.16137, "SK1 1SP", "Mersey Square, Stockport", "north-west"},
	"Tideswell Cinema":                 {53.27867, -1.77073, "SK17 8PD", "Tideswell Community Hall, Tideswell, Buxton", "east-midlands"},
	"HOME Manchester":                  {53.47293, -2.2466, "M15 4FN", "2 Tony Wilson Place, First Street, Manchester", "north-west"},
	"Showroom Sheffield":               {53.37706, -1.46566, "S1 2BX", "15 Paternoster Row, Sheffield", "yorkshire"},
	"Storyhouse Chester":               {53.19228, -2.89362, "CH1 2AR", "Hunter Street, Chester", "north-west"},
	"Altrincham Garrick Playhouse":     {53.39429, -2.35037, "WA14 1HZ", "Barrington Road, Altrincham", "north-west"},
	"Buxton Opera House":               {53.25857, -1.91579, "SK17 6AZ", "5 The Square, Buxton", "east-midlands"},
}
