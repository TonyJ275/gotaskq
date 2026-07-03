package worker

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Watchdog struct {
	pool     *pgxpool.Pool
	interval time.Duration
	timeout  time.Duration
}

func NewWatchdog(pool *pgxpool.Pool) *Watchdog {
	return &Watchdog{
		pool:     pool,
		interval: 1 * time.Minute, // check every minute
		timeout:  5 * time.Minute, // jobs running > 5 mins are stale
	}
}

func (wd *Watchdog) Start(ctx context.Context) {
	log.Println("Watchdog started")
	ticker := time.NewTicker(wd.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Watchdog stopped")
			return
		case <-ticker.C:
			wd.recoverStaleJobs(ctx)
		}
	}
}

func (wd *Watchdog) recoverStaleJobs(ctx context.Context) {
	result, err := wd.pool.Exec(ctx, `
		UPDATE jobs
		SET status = 'pending',
		    started_at = NULL,
		    updated_at = NOW()
		WHERE status = 'running'
		AND started_at < NOW() - $1::interval
	`, wd.timeout.String())

	if err != nil {
		log.Printf("Watchdog: failed to recover stale jobs: %v", err)
		return
	}

	if result.RowsAffected() > 0 {
		log.Printf("Watchdog: recovered %d stale jobs", result.RowsAffected())
	}
}
