// Package api implements the EventPulse HTTP management and ingestion API.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/eventpulse/eventpulse/internal/config"
	"github.com/eventpulse/eventpulse/internal/metrics"
	"github.com/eventpulse/eventpulse/internal/ratelimit"
	"github.com/eventpulse/eventpulse/internal/store"
)

type Server struct {
	cfg     *config.Config
	pool    *pgxpool.Pool
	q       *store.Queries
	limiter ratelimit.Limiter
	log     *slog.Logger
}

func NewServer(cfg *config.Config, pool *pgxpool.Pool, limiter ratelimit.Limiter, log *slog.Logger) *Server {
	return &Server{cfg: cfg, pool: pool, q: store.New(pool), limiter: limiter, log: log}
}

// Handler builds the routed handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.Handle("GET /metrics", promhttp.Handler())

	mux.HandleFunc("POST /v1/applications", s.createApplication)
	mux.HandleFunc("GET /v1/applications", s.listApplications)
	mux.HandleFunc("GET /v1/applications/{id}", s.getApplication)
	mux.HandleFunc("POST /v1/applications/{id}/events", s.ingestEvent)
	mux.HandleFunc("POST /v1/applications/{id}/endpoints", s.createEndpoint)

	mux.HandleFunc("GET /v1/endpoints", s.listEndpoints)
	mux.HandleFunc("PATCH /v1/endpoints/{id}", s.updateEndpoint)
	mux.HandleFunc("DELETE /v1/endpoints/{id}", s.deleteEndpoint)
	mux.HandleFunc("POST /v1/endpoints/{id}/rotate-secret", s.rotateSecret)

	mux.HandleFunc("GET /v1/deliveries", s.listDeliveries)
	mux.HandleFunc("POST /v1/deliveries/{id}/redeliver", s.redeliver)

	mux.HandleFunc("GET /v1/stats", s.stats)

	var h http.Handler = mux
	h = s.auth(h)
	h = s.cors(h)
	h = s.instrument(h)
	h = s.recoverer(h)
	return h
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

// ---- middleware ----

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }

func (s *Server) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(sw, r)
		// ServeMux fills r.Pattern in place, giving a low-cardinality route label.
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		if route == "GET /metrics" {
			return
		}
		metrics.HTTPRequests.WithLabelValues(r.Method, route, itoa(sw.code)).Inc()
		metrics.HTTPDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		s.log.Debug("request", "method", r.Method, "path", r.URL.Path, "status", sw.code, "dur", time.Since(start))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "err", rec, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	if s.cfg.APIKey == "" {
		return next
	}
	want := []byte(s.cfg.APIKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") && r.Method != http.MethodOptions {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
				writeError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	if s.cfg.CORSOrigin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", s.cfg.CORSOrigin)
		h.Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
