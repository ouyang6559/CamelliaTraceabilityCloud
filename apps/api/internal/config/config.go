// Package config loads runtime configuration from the environment (specs/08.7: no secrets in code).
package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Mode        string // run | seed | migrate
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string
	NATSURL     string
	MQTTURL     string

	JWTSecret          string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	SeedAdminPassword  string
	TraceCodeURLPrefix string // QR target, e.g. https://trace.example.com/t

	// Object storage: local disk by default; switch to s3 when StorageMode=s3.
	StorageMode  string // local | s3
	StorageDir   string
	S3Endpoint   string
	S3Bucket     string
	S3AccessKey  string
	S3SecretKey  string
	S3UseSSL     bool
	S3PublicBase string // optional public base URL for objects

	SyncInterval time.Duration
	AIProvider   string // mock | openai
	AIBaseURL    string
	AIAPIKey     string
	AIModel      string

	// Device adapter credentials. Each vendor adapter (adapters/device/<type>/)
	// reads its own block; a field left empty means that adapter stays
	// unregistered (specs/03.6, specs/06.11: never fabricate vendor APIs).
	// DJI Cloud API requires an enterprise account (Dock / 经纬系列) — see
	// https://developer.dji.com/doc/cloud-api-tutorial/en/. Integration TODO.
	DJIAppID     string
	DJIAppKey    string
	DJIAppSecret string
	DJIWorkspace string

	// SaaS multi-tenancy (docs/adr/adr-006): public self-registration toggle and
	// the platform administrator bootstrap password.
	RegistrationEnabled   bool
	PlatformAdminPassword string

	// Remote sensing (docs/adr/adr-008): NodeODM/WebODM processing node.
	// Empty URL = mock processor only (specs/04.9, specs/06.10).
	NodeODMURL   string
	NodeODMToken string

	// Weather (docs/adr/adr-010): mock | openmeteo (public API, no key).
	WeatherProvider string

	LogLevel string
}

func Load() *Config {
	c := &Config{
		Mode:                  getenv("CTC_MODE", "run"),
		HTTPAddr:              getenv("CTC_HTTP_ADDR", ":8080"),
		DatabaseURL:           getenv("CTC_DATABASE_URL", "postgres://ctc:ctc@localhost:5432/ctc?sslmode=disable"),
		RedisURL:              getenv("CTC_REDIS_URL", "redis://localhost:6379/0"),
		NATSURL:               getenv("CTC_NATS_URL", "nats://localhost:4222"),
		MQTTURL:               getenv("CTC_MQTT_URL", "tcp://localhost:1883"),
		JWTSecret:             getenv("CTC_JWT_SECRET", ""),
		AccessTokenTTL:        getDuration("CTC_ACCESS_TTL", 30*time.Minute),
		RefreshTokenTTL:       getDuration("CTC_REFRESH_TTL", 7*24*time.Hour),
		SeedAdminPassword:     getenv("CTC_SEED_ADMIN_PASSWORD", "admin123"),
		TraceCodeURLPrefix:    getenv("CTC_TRACE_URL_PREFIX", "http://localhost:5174/t"),
		StorageMode:           getenv("CTC_STORAGE_MODE", "local"),
		StorageDir:            getenv("CTC_STORAGE_DIR", "./data/files"),
		S3Endpoint:            getenv("CTC_S3_ENDPOINT", "localhost:9000"),
		S3Bucket:              getenv("CTC_S3_BUCKET", "ctc-files"),
		S3AccessKey:           getenv("CTC_S3_ACCESS_KEY", "ctc"),
		S3SecretKey:           getenv("CTC_S3_SECRET_KEY", "ctc-secret"),
		S3UseSSL:              getBool("CTC_S3_USE_SSL", false),
		SyncInterval:          getDuration("CTC_SYNC_INTERVAL", 15*time.Second),
		AIProvider:            getenv("CTC_AI_PROVIDER", "mock"),
		AIBaseURL:             getenv("CTC_AI_BASE_URL", ""),
		AIAPIKey:              getenv("CTC_AI_API_KEY", ""),
		AIModel:               getenv("CTC_AI_MODEL", "gpt-4o-mini"),
		DJIAppID:              getenv("CTC_DJI_APP_ID", ""),
		DJIAppKey:             getenv("CTC_DJI_APP_KEY", ""),
		DJIAppSecret:          getenv("CTC_DJI_APP_SECRET", ""),
		DJIWorkspace:          getenv("CTC_DJI_WORKSPACE", ""),
		RegistrationEnabled:   getBool("CTC_REGISTRATION_ENABLED", true),
		PlatformAdminPassword: getenv("CTC_PLATFORM_ADMIN_PASSWORD", "platform123"),
		NodeODMURL:            getenv("CTC_NODEODM_URL", ""),
		NodeODMToken:          getenv("CTC_NODEODM_TOKEN", ""),
		WeatherProvider:       getenv("CTC_WEATHER_PROVIDER", "openmeteo"),
		LogLevel:              getenv("CTC_LOG_LEVEL", "info"),
	}
	if c.JWTSecret == "" {
		// Development default; production must set CTC_JWT_SECRET (logged once at startup).
		c.JWTSecret = "dev-insecure-jwt-secret-change-me"
	}
	return c
}

func (c *Config) Logger() *slog.Logger {
	level := slog.LevelInfo
	switch c.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func getBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
