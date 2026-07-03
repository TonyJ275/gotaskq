package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TonyJ275/gotaskq/internal/model"
)

// JobHandler is a function that processes a job's payload. Each job type needs its own handler registered
type JobHandler func(ctx context.Context, payload []byte) error

// WorkerPool manages multiple concurrent workers
type WorkerPool struct {
	pool        *pgxpool.Pool
	concurrency int
	handlers    map[string]JobHandler
	wg          sync.WaitGroup
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(pool *pgxpool.Pool, concurrency int) *WorkerPool {
	return &WorkerPool{
		pool:        pool,
		concurrency: concurrency,
		handlers:    make(map[string]JobHandler),
	}
}

// RegisterHandler registers a handler function for a specific job type
func (wp *WorkerPool) RegisterHandler(jobType string, handler JobHandler) {
	wp.handlers[jobType] = handler
}

// Start launches all workers and blocks until context is cancelled
func (wp *WorkerPool) Start(ctx context.Context) {
	fmt.Printf("Starting worker pool with %d workers\n", wp.concurrency)

	for i := 0; i < wp.concurrency; i++ {
		wp.wg.Add(1)
		go wp.runWorker(ctx, i)
	}

	// Wait for context cancellation
	<-ctx.Done()
	fmt.Println("Shutting down worker pool...")

	// Wait for all workers to finish current jobs
	wp.wg.Wait()
	fmt.Println("Worker pool stopped")
}

// runWorker is the main loop for a single worker goroutine
func (wp *WorkerPool) runWorker(ctx context.Context, id int) {
	defer wp.wg.Done()

	log.Printf("Worker %d started", id)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Worker %d stopping", id)
			return
		default:
			job, err := wp.pollJob(ctx)
			if err != nil {
				log.Printf("Worker %d: error polling job: %v", id, err)
				wp.sleep(ctx, 5*time.Second)
				continue
			}

			if job == nil {
				// No jobs available, wait before polling again
				wp.sleep(ctx, 2*time.Second)
				continue
			}

			log.Printf("Worker %d: processing job %s (type: %s)", id, job.ID, job.Type)
			wp.processJob(ctx, job)
		}
	}
}

// pollJob atomically claims a pending job using SELECT FOR UPDATE SKIP LOCKED
// This is the core of the worker pool - ensures no two workers process the same job
func (wp *WorkerPool) pollJob(ctx context.Context) (*model.Job, error) {
	// Begin a transaction - the lock is held until transaction commits/rolls back
	tx, err := wp.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var job model.Job

	err = tx.QueryRow(ctx, `
		SELECT id, type, payload, status, priority, max_retries,
		       retry_count, error_message, scheduled_at, started_at,
		       completed_at, created_at, updated_at
		FROM jobs
		WHERE status = 'pending'
		AND scheduled_at <= NOW()
		ORDER BY priority DESC, scheduled_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(
		&job.ID,
		&job.Type,
		&job.Payload,
		&job.Status,
		&job.Priority,
		&job.MaxRetries,
		&job.RetryCount,
		&job.ErrorMessage,
		&job.ScheduledAt,
		&job.StartedAt,
		&job.CompletedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)

	if err != nil {
		// pgx returns this specific error when no rows found
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to poll job: %w", err)
	}

	// Mark job as running within the same transaction
	_, err = tx.Exec(ctx, `
		UPDATE jobs 
		SET status = 'running',
		    started_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, job.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to mark job as running: %w", err)
	}

	// Commit transaction - releases the lock
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	job.Status = model.JobStatusRunning
	return &job, nil
}

// processJob executes a job and handles success/failure
func (wp *WorkerPool) processJob(ctx context.Context, job *model.Job) {
	handler, exists := wp.handlers[job.Type]
	if !exists {
		errMsg := fmt.Sprintf("no handler registered for job type: %s", job.Type)
		log.Printf("Job %s failed: %s", job.ID, errMsg)
		wp.markFailed(ctx, job, errMsg)
		return
	}

	// Execute the handler
	err := handler(ctx, job.Payload)
	if err != nil {
		log.Printf("Job %s failed (attempt %d/%d): %v",
			job.ID, job.RetryCount+1, job.MaxRetries, err)

		if job.RetryCount >= job.MaxRetries {
			// No more retries - move to dead letter queue
			log.Printf("Job %s exceeded max retries, moving to dead", job.ID)
			wp.markDead(ctx, job, err.Error())
		} else {
			// Schedule retry with exponential backoff
			backoff := time.Duration(math.Pow(2, float64(job.RetryCount))) * time.Second
			log.Printf("Job %s retrying in %v", job.ID, backoff)
			wp.scheduleRetry(ctx, job, err.Error(), backoff)
		}
		return
	}

	// Success
	log.Printf("Job %s completed successfully", job.ID)
	wp.markCompleted(ctx, job)
}

// markCompleted marks a job as successfully completed
func (wp *WorkerPool) markCompleted(ctx context.Context, job *model.Job) {
	_, err := wp.pool.Exec(ctx, `
		UPDATE jobs
		SET status = 'completed',
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, job.ID)
	if err != nil {
		log.Printf("Failed to mark job %s as completed: %v", job.ID, err)
	}
}

// markFailed marks a job as failed
func (wp *WorkerPool) markFailed(ctx context.Context, job *model.Job, errMsg string) {
	_, err := wp.pool.Exec(ctx, `
		UPDATE jobs
		SET status = 'failed',
		    error_message = $2,
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, job.ID, errMsg)
	if err != nil {
		log.Printf("Failed to mark job %s as failed: %v", job.ID, err)
	}
}

// markDead moves a job to dead letter queue
func (wp *WorkerPool) markDead(ctx context.Context, job *model.Job, errMsg string) {
	_, err := wp.pool.Exec(ctx, `
		UPDATE jobs
		SET status = 'dead',
		    error_message = $2,
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, job.ID, errMsg)
	if err != nil {
		log.Printf("Failed to mark job %s as dead: %v", job.ID, err)
	}
}

// scheduleRetry requeues a failed job with exponential backoff
func (wp *WorkerPool) scheduleRetry(ctx context.Context, job *model.Job, errMsg string, backoff time.Duration) {
	_, err := wp.pool.Exec(ctx, `
		UPDATE jobs
		SET status = 'pending',
		    retry_count = retry_count + 1,
		    error_message = $2,
		    scheduled_at = NOW() + $3::interval,
		    started_at = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, job.ID, errMsg, backoff.String())
	if err != nil {
		log.Printf("Failed to schedule retry for job %s: %v", job.ID, err)
	}
}

// sleep pauses a worker while respecting context cancellation
// This is important - a regular time.Sleep would ignore context cancellation
// meaning workers wouldn't stop immediately when shutdown is requested
func (wp *WorkerPool) sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(d):
		return
	}
}
