// Package config loads service configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type Config struct {
	DatabaseURL string
	RedisURL    string // optional; rate limiting and locks are disabled when empty

	HTTPAddr    string // API listen address
	MetricsAddr string // worker metrics/health listen address
	APIKey      string // optional bearer token protecting /v1/*
	CORSOrigin  string // optional Access-Control-Allow-Origin value

	MigrateOnStart bool
	DefaultOwnerID uuid.UUID

	// Ingestion
	IngestRatePerSec int // events/sec per application (0 = unlimited)

	// Dispatcher
	WorkerConcurrency   int
	DispatchTimeout     time.Duration
	MaxAttempts         int
	BaseBackoff         time.Duration
	MaxBackoff          time.Duration
	PollInterval        time.Duration
	ClaimLease          time.Duration
	EndpointRatePerSec  int // deliveries/sec per endpoint (0 = unlimited)
	AllowPrivateTargets bool
}

func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:         env("DATABASE_URL", ""),
		RedisURL:            env("REDIS_URL", ""),
		HTTPAddr:            env("HTTP_ADDR", ":8080"),
		MetricsAddr:         env("METRICS_ADDR", ":9090"),
		APIKey:              env("API_KEY", ""),
		CORSOrigin:          env("CORS_ORIGIN", ""),
		MigrateOnStart:      envBool("MIGRATE_ON_START", false),
		IngestRatePerSec:    envInt("INGEST_RATE_PER_SEC", 200),
		WorkerConcurrency:   envInt("WORKER_CONCURRENCY", 32),
		DispatchTimeout:     envDuration("DISPATCH_TIMEOUT", 5*time.Second),
		MaxAttempts:         envInt("MAX_ATTEMPTS", 5),
		BaseBackoff:         envDuration("BASE_BACKOFF", 5*time.Second),
		MaxBackoff:          envDuration("MAX_BACKOFF", time.Hour),
		PollInterval:        envDuration("POLL_INTERVAL", 500*time.Millisecond),
		EndpointRatePerSec:  envInt("ENDPOINT_RATE_PER_SEC", 50),
		AllowPrivateTargets: envBool("ALLOW_PRIVATE_TARGETS", false),
	}
	c.ClaimLease = envDuration("CLAIM_LEASE", c.DispatchTimeout+30*time.Second)

	owner, err := uuid.Parse(env("DEFAULT_OWNER_ID", "00000000-0000-0000-0000-000000000001"))
	if err != nil {
		return nil, fmt.Errorf("DEFAULT_OWNER_ID: %w", err)
	}
	c.DefaultOwnerID = owner

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.WorkerConcurrency < 1 || c.MaxAttempts < 1 {
		return nil, fmt.Errorf("WORKER_CONCURRENCY and MAX_ATTEMPTS must be >= 1")
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := env(key, ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := env(key, ""); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := env(key, ""); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
