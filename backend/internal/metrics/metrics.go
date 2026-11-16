// Package metrics defines the Prometheus instrumentation shared by the API and worker.
package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	EventsIngested = promauto.NewCounter(prometheus.CounterOpts{
		Name: "eventpulse_events_ingested_total",
		Help: "Total events accepted by the ingestion API.",
	})

	DeliveriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "eventpulse_deliveries_total",
		Help: "Deliveries that reached a terminal state, by status.",
	}, []string{"status"})

	DeliveryRetries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "eventpulse_delivery_retries_total",
		Help: "Delivery attempts that failed and were scheduled for retry.",
	})

	DispatchDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "eventpulse_webhook_dispatch_duration_seconds",
		Help:    "Duration of outbound webhook HTTP requests.",
		Buckets: []float64{.025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	})

	QueueBacklog = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "eventpulse_queue_backlog_depth",
		Help: "Deliveries currently in the pending state.",
	})

	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "eventpulse_http_requests_total",
		Help: "API requests by method, route pattern and status code.",
	}, []string{"method", "route", "code"})

	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "eventpulse_http_request_duration_seconds",
		Help:    "API request latency by method and route pattern.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})
)

func init() {
	// Pre-create series so rate() works before the first observation.
	DeliveriesTotal.WithLabelValues("success")
	DeliveriesTotal.WithLabelValues("failed")
}

// BacklogSource reports the number of pending deliveries.
type BacklogSource interface {
	CountPendingDeliveries(ctx context.Context) (int64, error)
}

// WatchBacklog periodically refreshes the queue backlog gauge until ctx is done.
func WatchBacklog(ctx context.Context, src BacklogSource, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if n, err := src.CountPendingDeliveries(qctx); err == nil {
			QueueBacklog.Set(float64(n))
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
