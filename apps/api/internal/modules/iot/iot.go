// Package iot exposes the SensorThings-lite data model: Things(devices),
// Sensors, Datastreams, Observations (specs/04.3, specs/04.4).
package iot

import (
	"context"
	"net/http"

	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/apierr"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/ctxid"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/pkg/httpx"
)

// Module exposes read-oriented IoT endpoints (writes happen via telemetry ingest)
// and the weather integration (specs/04.5, docs/adr/adr-010).
type Module struct {
	pool    *pgxpool.Pool
	weather WeatherProvider
}

func NewModule(pool *pgxpool.Pool) *Module { return &Module{pool: pool} }

// SetWeatherProvider wires the weather provider (app assembly; required for /weather).
func (m *Module) SetWeatherProvider(p WeatherProvider) { m.weather = p }

// Mount registers /sensors, /datastreams, /observations and /weather.
// authorize wraps permission middleware — the audit (ADR-006) flagged that
// these read endpoints previously lacked per-route authorization.
func (m *Module) Mount(r chi.Router, authorize func(resource, action string) func(http.Handler) http.Handler) {
	read := authorize("observation", "read")
	r.Route("/sensors", func(rr chi.Router) {
		rr.Use(read)
		rr.Get("/", m.listSensors)
		rr.Get("/{id}", m.getSensor)
	})
	r.Route("/datastreams", func(rr chi.Router) {
		rr.Use(read)
		rr.Get("/", m.listDatastreams)
		rr.Get("/{id}", m.getDatastream)
		rr.Get("/{id}/observations", m.observationsForStream)
	})
	r.Route("/observations", func(rr chi.Router) {
		rr.Use(read)
		rr.Get("/", m.listObservations)
	})
	r.Route("/weather", func(rr chi.Router) {
		rr.Use(read)
		rr.Get("/current", m.weatherCurrent)
		rr.Get("/forecast", m.weatherForecast)
	})
}

// resolveFarmLocation returns the farm's point location (or boundary centroid)
// in WGS84, validating tenant ownership (ADR-006).
func (m *Module) resolveFarmLocation(ctx context.Context, pool *pgxpool.Pool, tenantID, farmID string) (lat, lng float64, err error) {
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(ST_Y(location), ST_Y(ST_Centroid(boundary)), 0),
		       COALESCE(ST_X(location), ST_X(ST_Centroid(boundary)), 0)
		FROM farms WHERE tenant_id=$1 AND id=$2`, tenantID, farmID).Scan(&lat, &lng)
	return
}

func (m *Module) weatherCurrent(w http.ResponseWriter, r *http.Request) {
	if err := m.weatherRespond(w, r, "current"); err != nil {
		httpx.Respond(w, r, nil, err)
	}
}

func (m *Module) weatherForecast(w http.ResponseWriter, r *http.Request) {
	if err := m.weatherRespond(w, r, "forecast"); err != nil {
		httpx.Respond(w, r, nil, err)
	}
}

// weatherRespond resolves the location (query lat/lng or farm_id) and serves
// current/forecast through the configured provider.
func (m *Module) weatherRespond(w http.ResponseWriter, r *http.Request, kind string) error {
	id, err := ctxid.Must(r.Context())
	if err != nil {
		return err
	}
	if m.weather == nil {
		return apierr.Unavailable("weather provider not configured")
	}
	q := r.URL.Query()
	var lat, lng float64
	if v := q.Get("lat"); v != "" {
		lat, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return apierr.Invalid("invalid lat")
		}
	}
	if v := q.Get("lng"); v != "" {
		lng, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return apierr.Invalid("invalid lng")
		}
	}
	if q.Get("farm_id") != "" {
		if err := tenantRefCheck(r, m.pool, id.TenantID, "farm", q.Get("farm_id")); err != nil {
			return err
		}
		lat, lng, err = m.resolveFarmLocation(r.Context(), m.pool, id.TenantID, q.Get("farm_id"))
		if err != nil {
			return apierr.NotFound("farm (or farm has no location)")
		}
	}
	if lat == 0 && lng == 0 {
		return apierr.Invalid("provide lat/lng or farm_id")
	}
	// WGS84 sanity.
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return apierr.Invalid("lat/lng out of range")
	}

	switch kind {
	case "current":
		cur, err := m.weather.Current(r.Context(), lat, lng)
		if err != nil {
			return apierr.Unavailable("weather fetch failed: %v", err)
		}
		cur.Source = m.weather.Name()
		httpx.Respond(w, r, map[string]any{
			"location": map[string]float64{"lat": lat, "lng": lng},
			"current":  cur,
		}, nil)
	case "forecast":
		days := 7
		if v := q.Get("days"); v != "" {
			if d, err := strconv.Atoi(v); err == nil && d >= 1 && d <= 16 {
				days = d
			}
		}
		f, err := m.weather.Forecast(r.Context(), lat, lng, days)
		if err != nil {
			return apierr.Unavailable("weather fetch failed: %v", err)
		}
		f.Source = m.weather.Name()
		httpx.Respond(w, r, map[string]any{
			"location": map[string]float64{"lat": lat, "lng": lng},
			"forecast": f,
		}, nil)
	}
	return nil
}

func (m *Module) listSensors(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	var total int64
	if err := m.pool.QueryRow(r.Context(), `SELECT count(*) FROM sensors WHERE tenant_id=$1`, id.TenantID).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	rows, err := m.pool.Query(r.Context(), `
		SELECT s.id, s.tenant_id, s.device_id, d.name AS device_name, s.name, s.description, s.created_at
		FROM sensors s LEFT JOIN devices d ON d.id = s.device_id
		WHERE s.tenant_id=$1 ORDER BY s.created_at DESC LIMIT $2 OFFSET $3`,
		id.TenantID, pp.PageSize, pp.Offset())
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func (m *Module) getSensor(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT s.id, s.tenant_id, s.device_id, d.name AS device_name, s.name, s.description, s.created_at
		FROM sensors s LEFT JOIN devices d ON d.id = s.device_id
		WHERE s.tenant_id=$1 AND s.id=$2`, id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	rows.Close()
	if err != nil {
		httpx.Respond(w, r, nil, apierr.NotFound("sensor"))
		return
	}
	httpx.Respond(w, r, item, nil)
}

func (m *Module) listDatastreams(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	args := []any{id.TenantID}
	conds := []string{"ds.tenant_id = $1"}
	if v := r.URL.Query().Get("device_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "ds.device_id = $"+itoa(len(args)))
	}
	if v := r.URL.Query().Get("observed_property"); v != "" {
		args = append(args, v)
		conds = append(conds, "ds.observed_property = $"+itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM datastreams ds WHERE "+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT ds.id, ds.tenant_id, ds.sensor_id, ds.device_id, d.name AS device_name,
			ds.name, ds.observed_property, ds.unit, ds.description, ds.created_at
		FROM datastreams ds LEFT JOIN devices d ON d.id = ds.device_id
		WHERE `+joinAnd(conds)+` ORDER BY ds.created_at DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func (m *Module) getDatastream(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	rows, err := m.pool.Query(r.Context(), `
		SELECT ds.id, ds.tenant_id, ds.sensor_id, ds.device_id, d.name AS device_name,
			ds.name, ds.observed_property, ds.unit, ds.description, ds.created_at
		FROM datastreams ds LEFT JOIN devices d ON d.id = ds.device_id
		WHERE ds.tenant_id=$1 AND ds.id=$2`, id.TenantID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	rows.Close()
	if err != nil {
		httpx.Respond(w, r, nil, apierr.NotFound("datastream"))
		return
	}
	httpx.Respond(w, r, item, nil)
}

// observationsForStream returns the time series for one datastream.
func (m *Module) observationsForStream(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	from, to := httpx.GetTime(r, "from"), httpx.GetTime(r, "to")
	args := []any{id.TenantID, chi.URLParam(r, "id")}
	conds := []string{"o.tenant_id = $1", "o.datastream_id = $2"}
	if !from.IsZero() {
		args = append(args, from)
		conds = append(conds, "o.phenomenon_time >= $"+itoa(len(args)))
	}
	if !to.IsZero() {
		args = append(args, to)
		conds = append(conds, "o.phenomenon_time <= $"+itoa(len(args)))
	}
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) FROM observations o WHERE "+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT o.id, o.datastream_id, o.device_id, o.result_number, o.result_string, o.result_json,
			o.phenomenon_time, o.result_time
		FROM observations o WHERE `+joinAnd(conds)+` ORDER BY o.phenomenon_time DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func (m *Module) listObservations(w http.ResponseWriter, r *http.Request) {
	id, _ := ctxid.Must(r.Context())
	pp := httpx.GetPageParams(r)
	q := r.URL.Query()
	args := []any{id.TenantID}
	conds := []string{"o.tenant_id = $1"}
	if v := q.Get("datastream_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "o.datastream_id = $"+itoa(len(args)))
	}
	if v := q.Get("device_id"); v != "" {
		args = append(args, v)
		conds = append(conds, "o.device_id = $"+itoa(len(args)))
	}
	if v := q.Get("observed_property"); v != "" {
		args = append(args, v)
		conds = append(conds, "ds.observed_property = $"+itoa(len(args)))
	}
	if !httpx.GetTime(r, "from").IsZero() {
		args = append(args, httpx.GetTime(r, "from"))
		conds = append(conds, "o.phenomenon_time >= $"+itoa(len(args)))
	}
	if !httpx.GetTime(r, "to").IsZero() {
		args = append(args, httpx.GetTime(r, "to"))
		conds = append(conds, "o.phenomenon_time <= $"+itoa(len(args)))
	}
	join := "FROM observations o LEFT JOIN datastreams ds ON ds.id = o.datastream_id WHERE "
	var total int64
	if err := m.pool.QueryRow(r.Context(),
		"SELECT count(*) "+join+joinAnd(conds), args...).Scan(&total); err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	args = append(args, pp.PageSize, pp.Offset())
	rows, err := m.pool.Query(r.Context(), `
		SELECT o.id, o.datastream_id, ds.observed_property, ds.unit, o.device_id,
			o.result_number, o.result_string, o.result_json, o.phenomenon_time, o.result_time
		`+join+joinAnd(conds)+` ORDER BY o.phenomenon_time DESC
		LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		httpx.Respond(w, r, nil, err)
		return
	}
	httpx.Respond(w, r, httpx.NewPage(items, total, pp), nil)
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }

// tenantRefCheck validates that a referenced object belongs to the tenant (ADR-006).
func tenantRefCheck(r *http.Request, pool *pgxpool.Pool, tenantID, refType, refID string) error {
	var table string
	switch refType {
	case "farm":
		table = "farms"
	default:
		return apierr.Invalid("unknown reference type %q", refType)
	}
	var n int
	if err := pool.QueryRow(r.Context(),
		"SELECT count(*) FROM "+table+" WHERE tenant_id=$1 AND id=$2", tenantID, refID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return apierr.Invalid("referenced %s does not exist in your tenant", refType)
	}
	return nil
}
