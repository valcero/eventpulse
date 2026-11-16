package main

import (
	"net/http"
	"os"
	"time"

	"github.com/eventpulse/eventpulse/db"
	"github.com/eventpulse/eventpulse/internal/api"
	"github.com/eventpulse/eventpulse/internal/config"
	"github.com/eventpulse/eventpulse/internal/metrics"
	"github.com/eventpulse/eventpulse/internal/platform"
	"github.com/eventpulse/eventpulse/internal/ratelimit"
	"github.com/eventpulse/eventpulse/internal/store"
)

func main() {
	log := platform.Logger()
	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	ctx, stop := platform.SignalContext()
	defer stop()

	if cfg.MigrateOnStart {
		if err := db.Migrate(cfg.DatabaseURL); err != nil {
			log.Error("migrate", "err", err)
			os.Exit(1)
		}
		log.Info("migrations applied")
	}

	pool, err := platform.OpenPool(ctx, cfg.DatabaseURL, log)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	limiter, err := ratelimit.New(cfg.RedisURL)
	if err != nil {
		log.Error("redis", "err", err)
		os.Exit(1)
	}
	defer limiter.Close()

	go metrics.WatchBacklog(ctx, store.New(pool), 5*time.Second)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewServer(cfg, pool, limiter, log).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Info("api listening", "addr", cfg.HTTPAddr)
	if err := platform.Serve(ctx, srv, log); err != nil && err != http.ErrServerClosed {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
