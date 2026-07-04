package db

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/TonyJ275/gotaskq/internal/model"
)

// setupTestDB spins up a real Postgres Docker container and applies the schema.
func setupTestDB(t *testing.T) (*JobRepository, *pgxpool.Pool, func()) {
	ctx := context.Background()

	// 1. Use the new postgres.Run() API
	pgContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
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

	repo := NewJobRepository(pool)

	cleanup := func() {
		pool.Close()
		_ = pgContainer.Terminate(context.Background())
	}

	return repo, pool, cleanup
}

func TestJobRepository_CreateAndGetJob(t *testing.T) {
	repo, _, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	req := model.CreateJobRequest{
		Type:       "email_job",
		Payload:    map[string]any{"to": "test@example.com"},
		Priority:   5,
		MaxRetries: 3,
	}

	// Test Create
	createdJob, err := repo.CreateJob(ctx, req)
	if err != nil {
		t.Fatalf("expected no error creating job, got: %v", err)
	}
	if createdJob.ID == uuid.Nil {
		t.Errorf("expected a valid UUID, got nil")
	}
	if createdJob.Status != "pending" {
		t.Errorf("expected status 'pending', got '%s'", createdJob.Status)
	}

	// Test Get
	fetchedJob, err := repo.GetJob(ctx, createdJob.ID)
	if err != nil {
		t.Fatalf("expected no error getting job, got: %v", err)
	}
	if fetchedJob.ID != createdJob.ID {
		t.Errorf("expected ID %s, got %s", createdJob.ID, fetchedJob.ID)
	}
}

func TestJobRepository_GetJob_NotFound(t *testing.T) {
	repo, _, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	randomID := uuid.New()
	_, err := repo.GetJob(ctx, randomID)
	if err == nil || err.Error() != "job not found" {
		t.Errorf("expected 'job not found' error, got: %v", err)
	}
}

func TestJobRepository_ListJobs(t *testing.T) {
	repo, _, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Insert jobs with different priorities
	repo.CreateJob(ctx, model.CreateJobRequest{Type: "job1", Priority: 1})
	repo.CreateJob(ctx, model.CreateJobRequest{Type: "job2", Priority: 10})
	repo.CreateJob(ctx, model.CreateJobRequest{Type: "job3", Priority: 5})

	// Assuming model.JobStatus is a string type
	jobs, err := repo.ListJobs(ctx, model.JobStatus("pending"), 2, 0)
	if err != nil {
		t.Fatalf("failed to list jobs: %v", err)
	}

	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs due to limit, got %d", len(jobs))
	}

	// Priority 10 should be first, then Priority 5
	if jobs[0].Priority != 10 {
		t.Errorf("expected first job to have priority 10, got %d", jobs[0].Priority)
	}
	if jobs[1].Priority != 5 {
		t.Errorf("expected second job to have priority 5, got %d", jobs[1].Priority)
	}
}

func TestJobRepository_GetStats(t *testing.T) {
	repo, pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Insert jobs
	job1, _ := repo.CreateJob(ctx, model.CreateJobRequest{Type: "test1"})
	repo.CreateJob(ctx, model.CreateJobRequest{Type: "test2"})

	// Manually change one to 'failed' to test aggregation
	_, _ = pool.Exec(ctx, "UPDATE jobs SET status = 'failed' WHERE id = $1", job1.ID)

	stats, err := repo.GetStats(ctx)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats["pending"] != 1 {
		t.Errorf("expected 1 pending job, got %d", stats["pending"])
	}
	if stats["failed"] != 1 {
		t.Errorf("expected 1 failed job, got %d", stats["failed"])
	}
}

func TestJobRepository_CancelJob(t *testing.T) {
	repo, _, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	job, _ := repo.CreateJob(ctx, model.CreateJobRequest{Type: "cancel_me"})

	err := repo.CancelJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("failed to cancel job: %v", err)
	}

	// Verify status changed
	updated, _ := repo.GetJob(ctx, job.ID)
	if updated.Status != "failed" {
		t.Errorf("expected status 'failed', got '%s'", updated.Status)
	}

	// Test cancelling an already failed job (should error)
	err = repo.CancelJob(ctx, job.ID)
	if err == nil {
		t.Errorf("expected error when cancelling non-pending job, got none")
	}
}

// TestDatabase_SkipLockedBehavior explicitly tests the Postgres locking logic
// exactly as it will be executed by your worker pool.
func TestDatabase_SkipLockedBehavior(t *testing.T) {
	repo, pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Insert a job
	job, _ := repo.CreateJob(ctx, model.CreateJobRequest{Type: "lock_test"})

	// 2. Start Transaction A (simulating Worker 1)
	tx1, _ := pool.Begin(ctx)
	defer tx1.Rollback(ctx)

	var lockedJobID uuid.UUID
	err := tx1.QueryRow(ctx, `
		SELECT id FROM jobs 
		WHERE status = 'pending' 
		FOR UPDATE SKIP LOCKED LIMIT 1
	`).Scan(&lockedJobID)

	if err != nil {
		t.Fatalf("Worker 1 failed to lock job: %v", err)
	}
	if lockedJobID != job.ID {
		t.Errorf("Worker 1 grabbed wrong job")
	}

	// 3. Start Transaction B (simulating Worker 2)
	tx2, _ := pool.Begin(ctx)
	defer tx2.Rollback(ctx)

	var missedJobID uuid.UUID
	err = tx2.QueryRow(ctx, `
		SELECT id FROM jobs 
		WHERE status = 'pending' 
		FOR UPDATE SKIP LOCKED LIMIT 1
	`).Scan(&missedJobID)

	// Worker 2 should get NO rows because Worker 1 has the only job locked
	if err == nil {
		t.Fatalf("Worker 2 should not have found a job, but it did")
	}
	if err.Error() != "no rows in result set" {
		t.Errorf("expected no rows error, got: %v", err)
	}
}

func TestJobRepository_DatabaseErrors(t *testing.T) {
	repo, _, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a context and cancel it immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 1. Force CreateJob to fail
	_, err := repo.CreateJob(ctx, model.CreateJobRequest{Type: "error_test"})
	if err == nil {
		t.Errorf("expected CreateJob to fail with cancelled context")
	}

	// 2. Force GetJob to fail
	_, err = repo.GetJob(ctx, uuid.New())
	if err == nil {
		t.Errorf("expected GetJob to fail with cancelled context")
	}

	// 3. Force ListJobs to fail
	_, err = repo.ListJobs(ctx, model.JobStatus("pending"), 10, 0)
	if err == nil {
		t.Errorf("expected ListJobs to fail with cancelled context")
	}

	// 4. Force GetStats to fail
	_, err = repo.GetStats(ctx)
	if err == nil {
		t.Errorf("expected GetStats to fail with cancelled context")
	}

	// 5. Force CancelJob to fail
	err = repo.CancelJob(ctx, uuid.New())
	if err == nil {
		t.Errorf("expected CancelJob to fail with cancelled context")
	}
}
