package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/TonyJ275/gotaskq/internal/metrics"
	"github.com/TonyJ275/gotaskq/internal/model"
)

// setupTestDB spins up a real Postgres Docker container and applies the schema.
func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	ctx := context.Background()

	// Updated to use the modern postgres.Run API
	pgContainer, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("user"),
		postgres.WithPassword("password"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}

	// Apply schema
	_, err = pool.Exec(ctx, `
        CREATE EXTENSION IF NOT EXISTS "pgcrypto";
        CREATE TABLE jobs (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            type VARCHAR(255) NOT NULL,
            payload JSONB NOT NULL,
            status VARCHAR(50) DEFAULT 'pending',
            priority INT DEFAULT 0,
            max_retries INT DEFAULT 3,
            retry_count INT DEFAULT 0,
            error_message TEXT,
            scheduled_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            started_at TIMESTAMP WITH TIME ZONE,
            completed_at TIMESTAMP WITH TIME ZONE,
            created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
        );
    `)
	if err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	cleanup := func() {
		pool.Close()
		_ = pgContainer.Terminate(context.Background())
	}

	return pool, cleanup
}

// insertTestJob is a helper to insert a job directly into the DB for testing
func insertTestJob(ctx context.Context, pool *pgxpool.Pool, jobType string) string {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO jobs (type, payload, status) 
		VALUES ($1, '{}', 'pending') 
		RETURNING id
	`, jobType).Scan(&id)
	if err != nil {
		panic(err)
	}
	return id
}

func TestWorkerPool_PollJob(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	testMetrics := metrics.New()
	wp := NewWorkerPool(pool, 1, testMetrics)

	// Insert a job
	insertTestJob(ctx, pool, "test_job")

	// Poll it
	job, err := wp.pollJob(ctx)
	if err != nil {
		t.Fatalf("expected no error polling job, got: %v", err)
	}
	if job == nil {
		t.Fatalf("expected a job, got nil")
	}
	if job.Status != model.JobStatusRunning {
		t.Errorf("expected job status to be running, got %s", job.Status)
	}
}

func TestWorkerPool_ProcessJob_Success(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	testMetrics := metrics.New()
	wp := NewWorkerPool(pool, 1, testMetrics)

	// Register a handler that always succeeds
	wp.RegisterHandler("success_job", func(ctx context.Context, payload []byte) error {
		return nil
	})

	id := insertTestJob(ctx, pool, "success_job")
	job, _ := wp.pollJob(ctx) // grab it to populate the struct

	// Process it
	wp.processJob(ctx, job)

	// Verify DB status is completed
	var status string
	err := pool.QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", id).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query status: %v", err)
	}
	if status != string(model.JobStatusCompleted) {
		t.Errorf("expected status 'completed', got '%s'", status)
	}
}

func TestWorkerPool_ProcessJob_MissingHandler(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	testMetrics := metrics.New()
	wp := NewWorkerPool(pool, 1, testMetrics)
	// Notice we DO NOT register a handler here

	id := insertTestJob(ctx, pool, "missing_handler_job")
	job, _ := wp.pollJob(ctx)

	wp.processJob(ctx, job)

	// Verify DB status is failed
	var status string
	err := pool.QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", id).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query status: %v", err)
	}
	if status != string(model.JobStatusFailed) {
		t.Errorf("expected status 'failed', got '%s'", status)
	}
}

func TestWorkerPool_ProcessJob_RetryLogic(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	testMetrics := metrics.New()
	wp := NewWorkerPool(pool, 1, testMetrics)

	// Register a handler that always fails
	wp.RegisterHandler("fail_job", func(ctx context.Context, payload []byte) error {
		return errors.New("simulated failure")
	})

	id := insertTestJob(ctx, pool, "fail_job")
	job, _ := wp.pollJob(ctx)

	// 1st failure (should retry)
	wp.processJob(ctx, job)

	var status string
	var retryCount int
	err := pool.QueryRow(ctx, "SELECT status, retry_count FROM jobs WHERE id = $1", id).Scan(&status, &retryCount)
	if err != nil {
		t.Fatalf("failed to query job: %v", err)
	}
	if status != string(model.JobStatusPending) {
		t.Errorf("expected status 'pending' (for retry), got '%s'", status)
	}
	if retryCount != 1 {
		t.Errorf("expected retry_count 1, got %d", retryCount)
	}

	// Manually force job to its max retries to test Dead Letter Queue
	_, err = pool.Exec(ctx, "UPDATE jobs SET retry_count = 3, max_retries = 3 WHERE id = $1", id)
	if err != nil {
		t.Fatalf("failed to update job: %v", err)
	}

	// Must update our local job struct to match the DB change
	job.RetryCount = 3
	job.MaxRetries = 3

	// Process final failure
	wp.processJob(ctx, job)

	err = pool.QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", id).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query job: %v", err)
	}
	if status != string(model.JobStatusDead) {
		t.Errorf("expected status 'dead', got '%s'", status)
	}
}

func TestWorkerPool_DatabaseErrors(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	testMetrics := metrics.New()
	wp := NewWorkerPool(pool, 1, testMetrics)

	// Create a context and cancel it immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 1. Force pollJob to fail (tx.Begin will fail due to canceled context)
	_, err := wp.pollJob(ctx)
	if err == nil {
		t.Errorf("expected error when polling with canceled context, got nil")
	}

	// Create a dummy job to pass to our helper methods
	dummyJob := &model.Job{} // ID will be a zero-value UUID, which is fine

	// 2. Force the helper methods to fail and hit their internal log.Printf blocks
	wp.markCompleted(ctx, dummyJob)
	wp.markFailed(ctx, dummyJob, "forced failure")
	wp.markDead(ctx, dummyJob, "forced dead")
	wp.scheduleRetry(ctx, dummyJob, "forced retry", time.Second)

	// 3. Test the sleep function's context cancellation path
	start := time.Now()
	wp.sleep(ctx, 10*time.Second) // Should return instantly, not in 10 seconds
	if time.Since(start) > time.Second {
		t.Errorf("sleep did not respect canceled context")
	}
}

func TestWorkerPool_StartAndShutdown(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	// Spin up a pool with 2 workers
	testMetrics := metrics.New()
	wp := NewWorkerPool(pool, 2, testMetrics)

	// Use a quick timeout context (100ms) to let the workers start up,
	// run a loop iteration, and then trigger a graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// This will block for 100ms and then return when shutdown completes safely
	wp.Start(ctx)
}
