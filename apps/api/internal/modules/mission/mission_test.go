package mission

import "testing"

func TestCapabilityFor(t *testing.T) {
	cases := map[string]string{
		"spray":     "SPRAY",
		"mow":       "MOW",
		"plow":      "PLOW",
		"seed":      "SEED",
		"fertilize": "FERTILIZE",
		"map":       "MAPPING",
		"inspect":   "INSPECTION",
		"transport": "AUTONOMOUS",
		"unknown":   "MISSION",
	}
	for mtype, want := range cases {
		if got := capabilityFor(mtype); got != want {
			t.Errorf("capabilityFor(%q) = %q, want %q", mtype, got, want)
		}
	}
}

func TestRiskLevelFor(t *testing.T) {
	high := []string{"spray", "mow", "plow", "seed"}
	for _, m := range high {
		if got := riskLevelFor(m); got != "high" {
			t.Errorf("riskLevelFor(%q) = %q, want high", m, got)
		}
	}
	if got := riskLevelFor("inspect"); got != "low" {
		t.Errorf("riskLevelFor(inspect) = %q, want low", got)
	}
}

func TestRouteWKT(t *testing.T) {
	route := []waypoint{{Lat: 28.1, Lng: 113.6}, {Lat: 28.2, Lng: 113.7}}
	wkt := routeWKT(route)
	if want := "LINESTRING(113.600000 28.100000, 113.700000 28.200000)"; wkt != want {
		t.Errorf("routeWKT = %q, want %q", wkt, want)
	}
	if got := routeWKT(nil); got != "" {
		t.Errorf("routeWKT(nil) = %q, want empty", got)
	}
}
