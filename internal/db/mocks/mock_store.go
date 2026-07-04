package db

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/TonyJ275/gotaskq/internal/db"
	"github.com/TonyJ275/gotaskq/internal/model"
)

type MockJobStore struct {
	Jobs        map[uuid.UUID]*model.Job
	CreateError error
	GetError    error
	ListError   error
	StatsError  error
	CancelError error
}

var _ db.JobStore = (*MockJobStore)(nil)

func NewMockJobStore() *MockJobStore {
	return &MockJobStore{
		Jobs: make(map[uuid.UUID]*model.Job),
	}
}

func (m *MockJobStore) CreateJob(ctx context.Context, req model.CreateJobRequest) (*model.Job, error) {
	if m.CreateError != nil {
		return nil, m.CreateError
	}

	payload, err := json.Marshal(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	now := time.Now()
	scheduledAt := now
	if req.ScheduledAt != nil {
		scheduledAt = *req.ScheduledAt
	}

	maxRetries := req.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	id := uuid.New()
	job := &model.Job{
		ID:          id,
		Type:        req.Type,
		Payload:     payload,
		Status:      model.JobStatusPending,
		Priority:    req.Priority,
		MaxRetries:  maxRetries,
		RetryCount:  0,
		ScheduledAt: scheduledAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	m.Jobs[id] = job
	return job, nil
}

func (m *MockJobStore) GetJob(ctx context.Context, id uuid.UUID) (*model.Job, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}

	job, exists := m.Jobs[id]
	if !exists {
		return nil, fmt.Errorf("job not found")
	}

	return job, nil
}

func (m *MockJobStore) ListJobs(ctx context.Context, status model.JobStatus, limit, offset int) ([]*model.Job, error) {
	if m.ListError != nil {
		return nil, m.ListError
	}

	// Filter by status
	var filtered []*model.Job
	for _, job := range m.Jobs {
		if job.Status == status {
			filtered = append(filtered, job)
		}
	}

	// Sort by priority DESC, scheduled_at ASC — matching DB ordering
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Priority != filtered[j].Priority {
			return filtered[i].Priority > filtered[j].Priority
		}
		return filtered[i].ScheduledAt.Before(filtered[j].ScheduledAt)
	})

	// Apply pagination
	start := offset
	if start >= len(filtered) {
		return []*model.Job{}, nil
	}

	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end], nil
}

func (m *MockJobStore) GetStats(ctx context.Context) (map[string]int, error) {
	if m.StatsError != nil {
		return nil, m.StatsError
	}

	stats := map[string]int{
		"pending":   0,
		"running":   0,
		"completed": 0,
		"failed":    0,
		"dead":      0,
	}

	for _, job := range m.Jobs {
		stats[string(job.Status)]++
	}

	return stats, nil
}

func (m *MockJobStore) CancelJob(ctx context.Context, id uuid.UUID) error {
	if m.CancelError != nil {
		return m.CancelError
	}

	job, exists := m.Jobs[id]
	if !exists {
		return fmt.Errorf("job not found or not in pending state")
	}

	if job.Status != model.JobStatusPending {
		return fmt.Errorf("job not found or not in pending state")
	}

	job.Status = model.JobStatusFailed
	job.UpdatedAt = time.Now()
	return nil
}
