// Package platform wires infrastructure connections (PostgreSQL/PostGIS, Redis, NATS, MQTT, storage).
package platform

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/config"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/migrations"
)

var migrationFS = migrations.FS

// NewDB opens the pgx pool and applies embedded SQL migrations (idempotent, ordered by filename).
func NewDB(ctx context.Context, cfg *config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolCfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := migrate(ctx, pool, log); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

type migration struct {
	name string
	sql  string
}

func migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	entries, err := migrationFS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	var migs []migration
	for _, e := range entries {
		data, err := migrationFS.ReadFile(e.Name())
		if err != nil {
			return err
		}
		migs = append(migs, migration{name: e.Name(), sql: string(data)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].name < migs[j].name })

	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, m := range migs {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, m.name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		bctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		_, err := pool.Exec(bctx, m.sql)
		cancel()
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, m.name); err != nil {
			return err
		}
		log.Info("migration applied", "name", m.name)
	}
	return nil
}
