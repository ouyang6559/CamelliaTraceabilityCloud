package iot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWeatherText covers WMO code mapping (specs/04.5 metrics stay labeled).
func TestWeatherText(t *testing.T) {
	cases := map[int]string{
		0: "晴", 2: "多云转晴", 3: "阴", 61: "降雨", 95: "雷暴", 99: "雷暴",
	}
	for code, want := range cases {
		if got := WeatherText(code); got != want {
			t.Errorf("WeatherText(%d) = %q, want %q", code, got, want)
		}
	}
	if WeatherText(1234) != "未知" {
		t.Error("unknown code should map to 未知")
	}
}

// openMeteoFixture mirrors the documented Open-Meteo response shape.
const openMeteoFixture = `{
  "current": {
    "time": "2026-11-28T12:00",
    "temperature_2m": 21.4,
    "relative_humidity_2m": 68,
    "precipitation": 0.3,
    "weather_code": 61,
    "wind_speed_10m": 11.2,
    "wind_direction_10m": 148,
    "surface_pressure": 1011.7
  },
  "daily": {
    "time": ["2026-11-28", "2026-11-29"],
    "temperature_2m_max": [25.1, 24.0],
    "temperature_2m_min": [16.2, 15.5],
    "precipitation_sum": [1.2, 0.0],
    "weather_code": [61, 0]
  }
}`

// TestOpenMeteoParse verifies the response parsing against the documented
// shape served by a test double (specs/06: implementation per public docs).
func TestOpenMeteoParse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openMeteoFixture))
	}))
	defer server.Close()

	p := &OpenMeteoProvider{BaseURL: server.URL}
	cur, err := p.Current(context.Background(), 28.19, 113.62)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if cur.Temperature != 21.4 || cur.Humidity != 68 || cur.WeatherCode != 61 {
		t.Errorf("current fields wrong: %+v", cur)
	}
	if cur.WeatherText != "降雨" {
		t.Errorf("weather text mapping wrong: %q", cur.WeatherText)
	}
	if cur.Source != "openmeteo" {
		t.Errorf("source should be openmeteo, got %q", cur.Source)
	}

	f, err := p.Forecast(context.Background(), 28.19, 113.62, 2)
	if err != nil {
		t.Fatalf("forecast: %v", err)
	}
	if len(f.Days) != 2 {
		t.Fatalf("expected 2 days, got %d", len(f.Days))
	}
	if f.Days[0].TempMax != 25.1 || f.Days[0].PrecipSum != 1.2 || f.Days[0].WeatherText != "降雨" {
		t.Errorf("day 0 wrong: %+v", f.Days[0])
	}
	if f.Days[1].WeatherText != "晴" {
		t.Errorf("day 1 wrong: %+v", f.Days[1])
	}
}

// TestOpenMeteoError verifies honest failure on upstream errors (specs/06.10).
func TestOpenMeteoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	p := &OpenMeteoProvider{BaseURL: server.URL}
	if _, err := p.Current(context.Background(), 28.19, 113.62); err == nil {
		t.Fatal("expected error on upstream 500")
	}
}

// TestMockDeterministicAndCached verifies mock determinism and the TTL cache
// hit path (CachedProvider must serve the same payload without re-calling).
func TestMockDeterministicAndCached(t *testing.T) {
	mock := &MockProvider{}
	a, _ := mock.Current(context.Background(), 28.19, 113.62)
	b, _ := mock.Current(context.Background(), 28.19, 113.62)
	if a.Temperature != b.Temperature || a.WeatherCode != b.WeatherCode {
		t.Error("mock provider must be deterministic")
	}

	calls := 0
	cached := NewCachedProvider(countingProvider{calls: &calls}, 5*time.Minute)
	ctx := context.Background()
	if _, err := cached.Current(ctx, 28.19, 113.62); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := cached.Current(ctx, 28.19, 113.62); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if calls != 1 {
		t.Errorf("cache should serve the second call: upstream calls=%d", calls)
	}
}

// TestCacheExpiry verifies the TTL actually expires.
func TestCacheExpiry(t *testing.T) {
	calls := 0
	cached := NewCachedProvider(countingProvider{calls: &calls}, 30*time.Millisecond)
	ctx := context.Background()
	_, _ = cached.Current(ctx, 28.19, 113.62)
	time.Sleep(60 * time.Millisecond)
	_, _ = cached.Current(ctx, 28.19, 113.62)
	if calls != 2 {
		t.Errorf("expired cache should refetch: calls=%d", calls)
	}
}

type countingProvider struct{ calls *int }

func (p countingProvider) Name() string { return "counting" }

func (p countingProvider) Current(_ context.Context, _, _ float64) (*WeatherCurrent, error) {
	*p.calls++
	return &WeatherCurrent{Temperature: 20, WeatherText: WeatherText(0)}, nil
}

func (p countingProvider) Forecast(_ context.Context, _, _ float64, days int) (*WeatherForecast, error) {
	return &WeatherForecast{Days: []WeatherDay{}}, nil
}
