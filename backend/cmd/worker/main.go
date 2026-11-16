package main

import (
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/eventpulse/eventpulse/internal/config"
	"github.com/eventpulse/eventpulse/internal/dispatcher"
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

	q := store.New(pool)

	// Metrics + health endpoint for Prometheus and Kubernetes probes.
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	srv := &http.Server{Addr: cfg.MetricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := platform.Serve(ctx, srv, log); err != nil && err != http.ErrServerClosed {
			log.Error("metrics server", "err", err)
			stop()
		}
	}()

	d := dispatcher.New(cfg, q, limiter, log)
	go metrics.WatchBacklog(ctx, q, 5*time.Second)
	go d.MaintainPartitions(ctx)

	log.Info("worker started", "concurrency", cfg.WorkerConcurrency, "max_attempts", cfg.MaxAttempts)
	d.Run(ctx)
	log.Info("worker stopped")
}
