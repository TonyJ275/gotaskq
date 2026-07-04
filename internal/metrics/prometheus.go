package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/TonyJ275/gotaskq/internal/db"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	JobsEnqueued  prometheus.Counter
	JobsCompleted prometheus.Counter
	JobsFailed    prometheus.Counter
	JobsDead      prometheus.Counter
	JobDuration   prometheus.Histogram
	QueueDepth    *prometheus.GaugeVec
	ActiveWorkers prometheus.Gauge
}

// -------------------------------------------------------------------------
// GLOBAL METRICS SINGLETON
// -------------------------------------------------------------------------
// Prometheus uses a global background registry by default (via promauto).
// If New() is called multiple times (like during sequential or parallel test runs),
// Prometheus will panic because it tries to register the same metric names twice.
// Using sync.Once guarantees these metrics are registered exactly one time per
// application lifecycle, making our code safe for both tests and production.
var (
	globalMetrics *Metrics
	once          sync.Once
)

func New() *Metrics {
	once.Do(func() {
		globalMetrics = &Metrics{
			JobsEnqueued: promauto.NewCounter(prometheus.CounterOpts{
				Name: "gotaskq_jobs_enqueued_total",
				Help: "Total number of jobs submitted to the queue",
			}),

			JobsCompleted: promauto.NewCounter(prometheus.CounterOpts{
				Name: "gotaskq_jobs_completed_total",
				Help: "Total number of jobs completed successfully",
			}),

			JobsFailed: promauto.NewCounter(prometheus.CounterOpts{
				Name: "gotaskq_jobs_failed_total",
				Help: "Total number of jobs that failed and were retried or dead lettered",
			}),

			JobsDead: promauto.NewCounter(prometheus.CounterOpts{
				Name: "gotaskq_jobs_dead_total",
				Help: "Total number of jobs moved to dead letter queue",
			}),

			JobDuration: promauto.NewHistogram(prometheus.HistogramOpts{
				Name:    "gotaskq_job_duration_seconds",
				Help:    "Time taken to process a job in seconds",
				Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
			}),

			QueueDepth: promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: "gotaskq_queue_depth",
				Help: "Current number of jobs in each status",
			}, []string{"status"}),

			ActiveWorkers: promauto.NewGauge(prometheus.GaugeOpts{
				Name: "gotaskq_active_workers",
				Help: "Number of workers currently processing jobs",
			}),
		}
	})

	return globalMetrics
}

func (m *Metrics) StartQueueDepthTracking(ctx context.Context, store db.JobStore) {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.updateQueueDepth(ctx, store)
			}
		}
	}()
}

func (m *Metrics) updateQueueDepth(ctx context.Context, store db.JobStore) {
	stats, err := store.GetStats(ctx)
	if err != nil {
		return
	}

	for status, count := range stats {
		m.QueueDepth.With(prometheus.Labels{"status": status}).Set(float64(count))
	}
}
