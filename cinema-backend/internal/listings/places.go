package listings

import "math"

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
