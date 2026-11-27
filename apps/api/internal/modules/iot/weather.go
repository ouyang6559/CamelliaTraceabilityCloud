// Weather integration (specs/04.5 气象, docs/adr/adr-010): pluggable providers
// over a platform location. The real provider calls Open-Meteo's public
// forecast API (https://open-meteo.com — free, no API key, fully documented),
// which covers every metric the spec requires: temperature, humidity,
// rainfall, wind speed/direction, pressure. A deterministic mock keeps CI and
// offline demos honest (specs/06.10: timeouts; specs/06.11: no fabricated
// endpoints). Responses are cached in-memory per provider+location with a TTL
// so dashboard/AI polling stays a good API citizen.
package iot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// WeatherCurrent is one nowcast snapshot (specs/04.5 metrics).
type WeatherCurrent struct {
	Time          string  `json:"time"`
	Temperature   float64 `json:"temperature"`   // °C
	Humidity      float64 `json:"humidity"`      // %
	Precipitation float64 `json:"precipitation"` // mm
	WeatherCode   int     `json:"weather_code"`
	WeatherText   string  `json:"weather_text"`
	WindSpeed     float64 `json:"wind_speed"`     // km/h
	WindDirection float64 `json:"wind_direction"` // degrees
	Pressure      float64 `json:"pressure"`       // hPa
	Source        string  `json:"source"`
}

// WeatherDay is one forecast day.
type WeatherDay struct {
	Date        string  `json:"date"`
	TempMax     float64 `json:"temp_max"`
	TempMin     float64 `json:"temp_min"`
	PrecipSum   float64 `json:"precip_sum"`
	WeatherCode int     `json:"weather_code"`
	WeatherText string  `json:"weather_text"`
}

// WeatherForecast is a multi-day forecast.
type WeatherForecast struct {
	Days   []WeatherDay `json:"days"`
	Source string       `json:"source"`
}

// WeatherProvider supplies current conditions and forecasts for a location.
type WeatherProvider interface {
	Name() string
	Current(ctx context.Context, lat, lng float64) (*WeatherCurrent, error)
	Forecast(ctx context.Context, lat, lng float64, days int) (*WeatherForecast, error)
}

// WeatherText maps WMO weather codes to short zh labels (frontend may localize).
func WeatherText(code int) string {
	switch code {
	case 0:
		return "晴"
	case 1, 2:
		return "多云转晴"
	case 3:
		return "阴"
	case 45, 48:
		return "雾"
	case 51, 53, 55, 56, 57:
		return "毛毛雨"
	case 61, 63, 65, 66, 67, 80, 81, 82:
		return "降雨"
	case 71, 73, 75, 77, 85, 86:
		return "降雪"
	case 95, 96, 99:
		return "雷暴"
	default:
		return "未知"
	}
}

// ---------- mock provider ----------

// MockProvider is deterministic (fixed values + slow day curve) for CI/offline.
type MockProvider struct{}

func (p *MockProvider) Name() string { return "mock" }

func (p *MockProvider) Current(_ context.Context, lat, lng float64) (*WeatherCurrent, error) {
	return &WeatherCurrent{
		Time:          time.Now().UTC().Format(time.RFC3339),
		Temperature:   24.5,
		Humidity:      72,
		Precipitation: 0,
		WeatherCode:   2,
		WeatherText:   WeatherText(2),
		WindSpeed:     8.4,
		WindDirection: 135,
		Pressure:      1009.2,
		Source:        "mock",
	}, nil
}

func (p *MockProvider) Forecast(_ context.Context, lat, lng float64, days int) (*WeatherForecast, error) {
	if days < 1 || days > 16 {
		days = 7
	}
	f := &WeatherForecast{Days: []WeatherDay{}, Source: "mock"}
	base := time.Now().UTC()
	for i := 0; i < days; i++ {
		code := []int{0, 2, 3, 61, 2, 1, 0}[i%7]
		f.Days = append(f.Days, WeatherDay{
			Date:        base.AddDate(0, 0, i).Format("2006-01-02"),
			TempMax:     28 - float64(i%3),
			TempMin:     18 + float64(i%2),
			PrecipSum:   []float64{0, 0, 0.2, 8.5, 0, 0, 0}[i%7],
			WeatherCode: code,
			WeatherText: WeatherText(code),
		})
	}
	return f, nil
}

// ---------- Open-Meteo provider (public API, specs/06) ----------

// OpenMeteoProvider calls https://api.open-meteo.com/v1/forecast — the public
// documented endpoint, no API key. Metrics requested match specs/04.5.
type OpenMeteoProvider struct {
	BaseURL string
	Client  *http.Client
}

func (p *OpenMeteoProvider) Name() string { return "openmeteo" }

func (p *OpenMeteoProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

const openMeteoMetrics = "temperature_2m,relative_humidity_2m,precipitation,weather_code,wind_speed_10m,wind_direction_10m,surface_pressure"

// openMeteoResponse mirrors the documented API shape.
type openMeteoResponse struct {
	Current struct {
		Time               string  `json:"time"`
		Temperature2m      float64 `json:"temperature_2m"`
		RelativeHumidity2m float64 `json:"relative_humidity_2m"`
		Precipitation      float64 `json:"precipitation"`
		WeatherCode        int     `json:"weather_code"`
		WindSpeed10m       float64 `json:"wind_speed_10m"`
		WindDirection10m   float64 `json:"wind_direction_10m"`
		SurfacePressure    float64 `json:"surface_pressure"`
	} `json:"current"`
	Daily struct {
		Time             []string  `json:"time"`
		Temperature2mMax []float64 `json:"temperature_2m_max"`
		Temperature2mMin []float64 `json:"temperature_2m_min"`
		PrecipitationSum []float64 `json:"precipitation_sum"`
		WeatherCode      []int     `json:"weather_code"`
	} `json:"daily"`
}

func (p *OpenMeteoProvider) fetch(ctx context.Context, lat, lng float64, days int) (*openMeteoResponse, error) {
	base := p.BaseURL
	if base == "" {
		base = "https://api.open-meteo.com/v1/forecast"
	}
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%f", lat))
	q.Set("longitude", fmt.Sprintf("%f", lng))
	q.Set("current", openMeteoMetrics)
	q.Set("daily", "temperature_2m_max,temperature_2m_min,precipitation_sum,weather_code")
	q.Set("forecast_days", fmt.Sprintf("%d", days))
	q.Set("timezone", "auto")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("open-meteo status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (p *OpenMeteoProvider) Current(ctx context.Context, lat, lng float64) (*WeatherCurrent, error) {
	parsed, err := p.fetch(ctx, lat, lng, 1)
	if err != nil {
		return nil, err
	}
	c := parsed.Current
	return &WeatherCurrent{
		Time:          c.Time,
		Temperature:   c.Temperature2m,
		Humidity:      c.RelativeHumidity2m,
		Precipitation: c.Precipitation,
		WeatherCode:   c.WeatherCode,
		WeatherText:   WeatherText(c.WeatherCode),
		WindSpeed:     c.WindSpeed10m,
		WindDirection: c.WindDirection10m,
		Pressure:      c.SurfacePressure,
		Source:        "openmeteo",
	}, nil
}

func (p *OpenMeteoProvider) Forecast(ctx context.Context, lat, lng float64, days int) (*WeatherForecast, error) {
	parsed, err := p.fetch(ctx, lat, lng, days)
	if err != nil {
		return nil, err
	}
	f := &WeatherForecast{Days: []WeatherDay{}, Source: "openmeteo"}
	d := parsed.Daily
	for i, date := range d.Time {
		day := WeatherDay{Date: date}
		if i < len(d.Temperature2mMax) {
			day.TempMax = d.Temperature2mMax[i]
		}
		if i < len(d.Temperature2mMin) {
			day.TempMin = d.Temperature2mMin[i]
		}
		if i < len(d.PrecipitationSum) {
			day.PrecipSum = d.PrecipitationSum[i]
		}
		if i < len(d.WeatherCode) {
			day.WeatherCode = d.WeatherCode[i]
			day.WeatherText = WeatherText(d.WeatherCode[i])
		}
		f.Days = append(f.Days, day)
	}
	return f, nil
}

// ---------- TTL cache wrapper ----------

type cacheEntry struct {
	payload  any
	expireAt time.Time
}

// CachedProvider wraps any WeatherProvider with an in-memory TTL cache
// (single-node modular monolith; multi-node can move this to Redis later).
type CachedProvider struct {
	inner WeatherProvider
	ttl   time.Duration

	mu    sync.Mutex
	items map[string]cacheEntry
}

func NewCachedProvider(inner WeatherProvider, ttl time.Duration) *CachedProvider {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &CachedProvider{inner: inner, ttl: ttl, items: map[string]cacheEntry{}}
}

func (c *CachedProvider) Name() string { return c.inner.Name() }

func (c *CachedProvider) key(kind string, lat, lng float64, days int) string {
	return fmt.Sprintf("%s|%.4f|%.4f|%d", kind, lat, lng, days)
}

func (c *CachedProvider) get(key string, dst any) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok || time.Now().After(e.expireAt) {
		return false
	}
	raw, err := json.Marshal(e.payload)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

func (c *CachedProvider) put(key string, payload any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cacheEntry{payload: payload, expireAt: time.Now().Add(c.ttl)}
}

func (c *CachedProvider) Current(ctx context.Context, lat, lng float64) (*WeatherCurrent, error) {
	key := c.key("current", lat, lng, 0)
	var cached WeatherCurrent
	if c.get(key, &cached) {
		return &cached, nil
	}
	cur, err := c.inner.Current(ctx, lat, lng)
	if err != nil {
		return nil, err
	}
	c.put(key, cur)
	return cur, nil
}

func (c *CachedProvider) Forecast(ctx context.Context, lat, lng float64, days int) (*WeatherForecast, error) {
	key := c.key("forecast", lat, lng, days)
	var cached WeatherForecast
	if c.get(key, &cached) {
		return &cached, nil
	}
	f, err := c.inner.Forecast(ctx, lat, lng, days)
	if err != nil {
		return nil, err
	}
	c.put(key, f)
	return f, nil
}
