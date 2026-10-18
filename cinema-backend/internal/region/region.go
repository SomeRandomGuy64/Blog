// Package region is the fixed set of UK regions the listings are split into: England's nine
// ITL1 regions plus Scotland, Wales and Northern Ireland.
package region

// Region is one selectable area; Key is what the API and the registry use.
type Region struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// All is every region, roughly north to south.
var All = []Region{
	{"scotland", "Scotland"},
	{"northern-ireland", "Northern Ireland"},
	{"north-east", "North East"},
	{"north-west", "North West"},
	{"yorkshire", "Yorkshire and the Humber"},
	{"east-midlands", "East Midlands"},
	{"west-midlands", "West Midlands"},
	{"wales", "Wales"},
	{"east", "East of England"},
	{"london", "London"},
	{"south-east", "South East"},
	{"south-west", "South West"},
}

// Default is what the API serves when the request names no region (the original coverage).
var Default = []string{"north-west", "yorkshire", "east-midlands"}

// Valid reports whether key is a region key.
func Valid(key string) bool {
	for _, r := range All {
		if r.Key == key {
			return true
		}
	}
	return false
}

// Name returns a key's display name ("" if unknown).
func Name(key string) string {
	for _, r := range All {
		if r.Key == key {
			return r.Name
		}
	}
	return ""
}

// FromPostcodesIO maps a postcodes.io result's region (England only) and country to a key;
// "" when it is outside the UK or unrecognised.
func FromPostcodesIO(regionName, country string) string {
	switch country {
	case "Scotland":
		return "scotland"
	case "Wales":
		return "wales"
	case "Northern Ireland":
		return "northern-ireland"
	case "England":
	default:
		return ""
	}
	switch regionName {
	case "North East":
		return "north-east"
	case "North West":
		return "north-west"
	case "Yorkshire and The Humber", "Yorkshire and the Humber":
		return "yorkshire"
	case "East Midlands":
		return "east-midlands"
	case "West Midlands":
		return "west-midlands"
	case "East of England":
		return "east"
	case "London":
		return "london"
	case "South East":
		return "south-east"
	case "South West":
		return "south-west"
	}
	return ""
}
