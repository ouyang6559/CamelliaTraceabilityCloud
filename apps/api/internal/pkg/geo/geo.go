// Package geo provides GeoJSON <-> PostGIS helpers and lightweight geometry utilities.
package geo

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
)

// Geometry is a GeoJSON geometry (Point/Polygon/MultiPolygon/LineString ...) exchanged as raw GeoJSON.
// Stores must bind it via ST_GeomFromGeoJSON($n, 4326) and read via ST_AsGeoJSON(col).
type Geometry json.RawMessage

func (g Geometry) MarshalJSON() ([]byte, error) {
	if len(g) == 0 {
		return []byte("null"), nil
	}
	return g, nil
}

func (g *Geometry) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*g = nil
		return nil
	}
	var probe map[string]any
	if err := json.Unmarshal(data, &probe); err != nil {
		return apierr.Invalid("invalid GeoJSON geometry")
	}
	if t, _ := probe["type"].(string); t == "" {
		return apierr.Invalid("GeoJSON geometry requires \"type\"")
	}
	*g = Geometry(data)
	return nil
}

// Validate checks that the geometry carries a type field (full validation happens in PostGIS).
func (g Geometry) Validate() error {
	if len(g) == 0 {
		return nil
	}
	var probe struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(g, &probe); err != nil || probe.Type == "" || probe.Coordinates == nil {
		return apierr.Invalid("invalid GeoJSON geometry")
	}
	return nil
}

// Point is a WGS84 coordinate used across devices, tasks and trace events.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// IsValid reports whether the point lies within WGS84 bounds.
func (p Point) IsValid() bool {
	return p.Lat >= -90 && p.Lat <= 90 && p.Lng >= -180 && p.Lng <= 180
}

// WKT renders the point as WKT (SRID 4326).
func (p Point) WKT() string { return fmt.Sprintf("POINT(%f %f)", p.Lng, p.Lat) }

// HaversineMeters returns great-circle distance in meters.
func HaversineMeters(a, b Point) float64 {
	const r = 6371000.0
	rad := math.Pi / 180
	dLat := (b.Lat - a.Lat) * rad
	dLng := (b.Lng - a.Lng) * rad
	s := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(s))
}

// NullGeometry returns a driver value usable with pgx for optional geometry columns.
func NullGeometry(g Geometry) driver.Value {
	if len(g) == 0 {
		return nil
	}
	return string(g)
}
