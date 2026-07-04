package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/TonyJ275/gotaskq/internal/model"
)

// JobStore defines all database operations for jobs
// This interface allows us to mock the database in tests
type JobStore interface {
	CreateJob(ctx context.Context, req model.CreateJobRequest) (*model.Job, error)
	GetJob(ctx context.Context, id uuid.UUID) (*model.Job, error)
	ListJobs(ctx context.Context, status model.JobStatus, limit, offset int) ([]*model.Job, error)
	GetStats(ctx context.Context) (map[string]int, error)
	CancelJob(ctx context.Context, id uuid.UUID) error
}
