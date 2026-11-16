// Package dispatcher implements the concurrent webhook delivery worker pool.
package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/eventpulse/eventpulse/internal/config"
	"github.com/eventpulse/eventpulse/internal/metrics"
	"github.com/eventpulse/eventpulse/internal/ratelimit"
	"github.com/eventpulse/eventpulse/internal/store"
)

type Dispatcher struct {
	cfg     *config.Config
	q       *store.Queries
	limiter ratelimit.Limiter
	client  *http.Client
	log     *slog.Logger
}

func New(cfg *config.Config, q *store.Queries, limiter ratelimit.Limiter, log *slog.Logger) *Dispatcher {
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	if !cfg.AllowPrivateTargets {
		// Control runs after DNS resolution, which also defeats DNS rebinding.
		dialer.Control = blockPrivate
	}
	return &Dispatcher{
		cfg:     cfg,
		q:       q,
		limiter: limiter,
		log:     log,
		client: &http.Client{
			Timeout: cfg.DispatchTimeout,
			Transport: &http.Transport{
				DialContext:         dialer.DialContext,
				MaxIdleConns:        256,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 3 * time.Second,
			},
			// Never follow redirects: they could bounce to internal addresses.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func blockPrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return fmt.Errorf("destination %s is not permitted", host)
	}
	return nil
}

// Run polls for due deliveries and dispatches them on a pool of goroutines.
// It returns after ctx is cancelled and all in-flight deliveries finish.
func (d *Dispatcher) Run(ctx context.Context) {
	jobs := make(chan store.ClaimPendingDeliveriesRow, d.cfg.WorkerConcurrency)
	var wg sync.WaitGroup
	for i := 0; i < d.cfg.WorkerConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				d.process(row)
			}
		}()
	}

	for ctx.Err() == nil {
		// Only this goroutine sends, so claiming at most `free` rows guarantees
		// the sends below never block.
		free := cap(jobs) - len(jobs)
		if free > 0 {
			rows, err := d.q.ClaimPendingDeliveries(ctx, store.ClaimPendingDeliveriesParams{
				BatchSize:    int32(free),
				LeaseSeconds: d.cfg.ClaimLease.Seconds(),
			})
			if err != nil {
				if ctx.Err() == nil {
					d.log.Error("claim deliveries", "err", err)
				}
			} else {
				for _, r := range rows {
					jobs <- r
				}
				if len(rows) == free {
					continue // more work is probably waiting
				}
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(d.cfg.PollInterval):
		}
	}
	close(jobs)
	wg.Wait()
}

type envelope struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (d *Dispatcher) process(row store.ClaimPendingDeliveriesRow) {
	// Writes use their own context so results survive a shutdown signal.
	wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	log := d.log.With("delivery_id", row.ID, "endpoint_id", row.EndpointID)

	if !row.IsActive {
		d.finishFailed(wctx, log, row, nil)
		return
	}
	if !d.limiter.Allow(wctx, row.EndpointID.String(), d.cfg.EndpointRatePerSec) {
		next := time.Now().Add(time.Second + Backoff(1, 250*time.Millisecond, time.Second))
		if err := d.q.DeferDelivery(wctx, store.DeferDeliveryParams{ID: row.ID, CreatedAt: row.CreatedAt, NextRetryAt: next}); err != nil {
			log.Error("defer delivery", "err", err)
		}
		return
	}

	code, err := d.send(row)
	attempts := int(row.Attempts) + 1
	switch {
	case err == nil && code >= 200 && code < 300:
		if e := d.q.MarkDeliverySuccess(wctx, store.MarkDeliverySuccessParams{ID: row.ID, CreatedAt: row.CreatedAt, StatusCode: &code}); e != nil {
			log.Error("mark success", "err", e)
			return
		}
		metrics.DeliveriesTotal.WithLabelValues("success").Inc()
	case attempts >= d.cfg.MaxAttempts:
		var cp *int32
		if code != 0 {
			cp = &code
		}
		log.Warn("delivery failed permanently", "attempts", attempts, "status", code, "err", err)
		d.finishFailed(wctx, log, row, cp)
	default:
		var cp *int32
		if code != 0 {
			cp = &code
		}
		next := time.Now().Add(Backoff(attempts, d.cfg.BaseBackoff, d.cfg.MaxBackoff))
		if e := d.q.MarkDeliveryRetry(wctx, store.MarkDeliveryRetryParams{ID: row.ID, CreatedAt: row.CreatedAt, StatusCode: cp, NextRetryAt: next}); e != nil {
			log.Error("mark retry", "err", e)
			return
		}
		metrics.DeliveryRetries.Inc()
		log.Info("delivery will retry", "attempt", attempts, "status", code, "err", err, "next", next)
	}
}

func (d *Dispatcher) finishFailed(ctx context.Context, log *slog.Logger, row store.ClaimPendingDeliveriesRow, code *int32) {
	if err := d.q.MarkDeliveryFailed(ctx, store.MarkDeliveryFailedParams{ID: row.ID, CreatedAt: row.CreatedAt, StatusCode: code}); err != nil {
		log.Error("mark failed", "err", err)
		return
	}
	metrics.DeliveriesTotal.WithLabelValues("failed").Inc()
}

// send performs one signed POST. It returns the HTTP status (0 if none).
func (d *Dispatcher) send(row store.ClaimPendingDeliveriesRow) (int32, error) {
	body, err := json.Marshal(envelope{ID: row.EventID.String(), Type: row.EventType, Data: row.Payload})
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), d.cfg.DispatchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, row.Url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	now := time.Now()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "EventPulse/1.0")
	req.Header.Set(SignatureHeader, Sign(row.Secret, now, body))
	req.Header.Set("X-EventPulse-Event", row.EventType)
	req.Header.Set("X-EventPulse-Delivery-Id", row.ID.String())

	start := time.Now()
	resp, err := d.client.Do(req)
	metrics.DispatchDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 64<<10) // drain for connection reuse
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return int32(resp.StatusCode), errors.New(resp.Status)
	}
	return int32(resp.StatusCode), nil
}

// MaintainPartitions ensures monthly delivery partitions exist for the next
// few months. A Redis lock keeps multiple workers from racing.
func (d *Dispatcher) MaintainPartitions(ctx context.Context) {
	run := func() {
		if !d.limiter.TryLock(ctx, "partition-maintenance", 30*time.Minute) {
			return
		}
		for m := 0; m <= 3; m++ {
			if _, err := d.q.EnsureDeliveriesPartition(ctx, pgtype.Date{Time: time.Now().AddDate(0, m, 0), Valid: true}); err != nil {
				d.log.Error("ensure partition", "months_ahead", m, "err", err)
			}
		}
	}
	run()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
