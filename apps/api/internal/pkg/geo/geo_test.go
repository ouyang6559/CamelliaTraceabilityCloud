package geo

import (
	"testing"
)

func TestPointValidity(t *testing.T) {
	valid := Point{Lat: 28.19, Lng: 113.62}
	if !valid.IsValid() {
		t.Fatal("valid point rejected")
	}
	invalid := Point{Lat: 120, Lng: 113.62}
	if invalid.IsValid() {
		t.Fatal("invalid point accepted")
	}
}

func TestHaversineMeters(t *testing.T) {
	// Changsha (113.6285, 28.1925) -> Liuyang area (~11km apart)
	a := Point{Lat: 28.1925, Lng: 113.6285}
	b := Point{Lat: 28.1925, Lng: 113.75}
	d := HaversineMeters(a, b)
	if d < 10000 || d > 13000 {
		t.Fatalf("distance out of expected range: %f", d)
	}
	if HaversineMeters(a, a) > 0.001 {
		t.Fatal("zero distance expected")
	}
}

func TestGeometryValidation(t *testing.T) {
	var g Geometry
	if err := g.Validate(); err != nil {
		t.Fatalf("nil geometry should pass: %v", err)
	}
	g = Geometry(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`)
	if err := g.Validate(); err != nil {
		t.Fatalf("valid polygon rejected: %v", err)
	}
	g = Geometry(`{"type":"Polygon"}`)
	if err := g.Validate(); err == nil {
		t.Fatal("polygon without coordinates should fail")
	}
}
