package platform

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/config"
)

// NewRedis connects to Redis; failure is fatal because rate limiting and caching rely on it.
func NewRedis(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opts)
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}

// NewNATS connects to NATS with a short retry window.
func NewNATS(ctx context.Context, cfg *config.Config, log *slog.Logger) (*nats.Conn, error) {
	var conn *nats.Conn
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		conn, err = nats.Connect(cfg.NATSURL, nats.Timeout(3*time.Second), nats.Name("ctc-api"))
		if err == nil {
			return conn, nil
		}
		log.Warn("nats connect failed, retrying", "attempt", attempt, "err", err)
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return nil, fmt.Errorf("connect nats: %w", err)
}
