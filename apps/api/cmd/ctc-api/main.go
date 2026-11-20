// Command ctc-api is the CamelliaTraceabilityCloud API server (modular monolith).
// Modes: run (default) | seed (load demo dataset) | migrate (apply schema only).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/app"
	"github.com/ouyang6559/CamelliaTraceabilityCloud/apps/api/internal/config"
)

func main() {
	mode := flag.String("mode", "", "run | seed | migrate (overrides CTC_MODE)")
	flag.Parse()

	cfg := config.Load()
	if *mode != "" {
		cfg.Mode = *mode
	}
	log := cfg.Logger()
	log.Info("camellia traceability cloud api", "mode", cfg.Mode, "addr", cfg.HTTPAddr)

	if cfg.JWTSecret == "dev-insecure-jwt-secret-change-me" {
		log.Warn("CTC_JWT_SECRET not set; using development default — do not use in production")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Mode {
	case "migrate":
		if err := app.RunMigrate(ctx, cfg, log); err != nil {
			log.Error("migrate failed", "err", err)
			os.Exit(1)
		}
		log.Info("migrations complete")
		return
	case "seed":
		if err := app.RunSeed(ctx, cfg, log); err != nil {
			log.Error("seed failed", "err", err)
			os.Exit(1)
		}
		log.Info("seed complete")
		return
	}

	a, err := app.New(ctx, cfg, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	a.StartSyncWorker(ctx)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Info("api listening", "addr", fmt.Sprintf("http://%s", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	a.Close()
	log.Info("bye")
}
