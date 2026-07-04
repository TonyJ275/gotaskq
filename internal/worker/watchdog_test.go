package worker

import (
	"context"
	"testing"
	"time"

	"github.com/TonyJ275/gotaskq/internal/model"
)

func TestWatchdog_RecoverStaleJobs(t *testing.T) {
	// Reuses the setupTestDB function directly from pool_test.go
	pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Insert two jobs: one that is stale (stuck for 10 mins) and one that just started
	var staleJobID string
	err := pool.QueryRow(ctx, `
		INSERT INTO jobs (type, payload, status, started_at) 
		VALUES ('stale_job', '{}', 'running', NOW() - INTERVAL '10 minutes') 
		RETURNING id
	`).Scan(&staleJobID)
	if err != nil {
		t.Fatalf("failed to insert stale job: %v", err)
	}

	var activeJobID string
	err = pool.QueryRow(ctx, `
		INSERT INTO jobs (type, payload, status, started_at) 
		VALUES ('active_job', '{}', 'running', NOW()) 
		RETURNING id
	`).Scan(&activeJobID)
	if err != nil {
		t.Fatalf("failed to insert active job: %v", err)
	}

	// 2. Initialize the watchdog (default timeout is 5 minutes, so 10 minutes is stale)
	wd := NewWatchdog(pool)

	// 3. Trigger the recovery logic directly
	wd.recoverStaleJobs(ctx)

	// 4. Verify the stale job was moved back to 'pending'
	var staleStatus string
	err = pool.QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", staleJobID).Scan(&staleStatus)
	if err != nil {
		t.Fatalf("failed to query stale job: %v", err)
	}
	if staleStatus != string(model.JobStatusPending) {
		t.Errorf("expected stale job status to be 'pending', got '%s'", staleStatus)
	}

	// 5. Verify the active job was NOT touched and remains 'running'
	var activeStatus string
	err = pool.QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", activeJobID).Scan(&activeStatus)
	if err != nil {
		t.Fatalf("failed to query active job: %v", err)
	}
	if activeStatus != string(model.JobStatusRunning) {
		t.Errorf("expected active job status to remain 'running', got '%s'", activeStatus)
	}
}

func TestWatchdog_DatabaseErrors(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	wd := NewWatchdog(pool)

	// Create a context and cancel it immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Trigger recovery with the dead context to force the query to fail
	// This will hit the `if err != nil` log block inside recoverStaleJobs
	wd.recoverStaleJobs(ctx)
}

func TestWatchdog_StartAndShutdown(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	wd := NewWatchdog(pool)
	// Force the interval to be incredibly fast (10ms) so the ticker
	// actually fires during our short test window
	wd.interval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Runs the loop, hits the ticker, and gracefully exits when context expires
	wd.Start(ctx)
}
