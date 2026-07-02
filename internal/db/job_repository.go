package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TonyJ275/gotaskq/internal/model"
)

type JobRepository struct {
	pool *pgxpool.Pool
}

func NewJobRepository(pool *pgxpool.Pool) *JobRepository {
	return &JobRepository{pool: pool}
}

// CreateJob inserts a new job into the database
func (r *JobRepository) CreateJob(ctx context.Context, req model.CreateJobRequest) (*model.Job, error) {
	payload, err := json.Marshal(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	scheduledAt := time.Now()
	if req.ScheduledAt != nil {
		scheduledAt = *req.ScheduledAt
	}

	maxRetries := req.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	var job model.Job
	err = r.pool.QueryRow(ctx, `
		INSERT INTO jobs (type, payload, priority, max_retries, scheduled_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, type, payload, status, priority, max_retries, 
		          retry_count, error_message, scheduled_at, started_at, 
		          completed_at, created_at, updated_at
	`, req.Type, payload, req.Priority, maxRetries, scheduledAt).Scan(
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
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	return &job, nil
}

// GetJob retrieves a single job by ID
func (r *JobRepository) GetJob(ctx context.Context, id uuid.UUID) (*model.Job, error) {
	var job model.Job
	err := r.pool.QueryRow(ctx, `
		SELECT id, type, payload, status, priority, max_retries,
		       retry_count, error_message, scheduled_at, started_at,
		       completed_at, created_at, updated_at
		FROM jobs
		WHERE id = $1
	`, id).Scan(
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
		// handle "not found" case explicitly
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("job not found")
		}

		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	return &job, nil
}

// ListJobs retrieves jobs filtered by status with pagination
func (r *JobRepository) ListJobs(ctx context.Context, status model.JobStatus, limit, offset int) ([]*model.Job, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, type, payload, status, priority, max_retries,
		       retry_count, error_message, scheduled_at, started_at,
		       completed_at, created_at, updated_at
		FROM jobs
		WHERE status = $1
		ORDER BY priority DESC, scheduled_at ASC
		LIMIT $2 OFFSET $3
	`, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*model.Job
	for rows.Next() {
		var job model.Job
		err := rows.Scan(
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
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return jobs, nil
}

// GetStats returns count of jobs grouped by status
func (r *JobRepository) GetStats(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT status, COUNT(*) 
		FROM jobs 
		GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	defer rows.Close()

	stats := map[string]int{
		"pending":   0,
		"running":   0,
		"completed": 0,
		"failed":    0,
		"dead":      0,
	}

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan stats: %w", err)
		}
		stats[status] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return stats, nil
}

// CancelJob cancels a pending job
func (r *JobRepository) CancelJob(ctx context.Context, id uuid.UUID) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE jobs 
		SET status = 'failed',
		    error_message = 'cancelled by user',
		    updated_at = NOW()
		WHERE id = $1 
		AND status = 'pending'
	`, id)
	if err != nil {
		return fmt.Errorf("failed to cancel job: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found or not in pending state")
	}

	return nil
}
