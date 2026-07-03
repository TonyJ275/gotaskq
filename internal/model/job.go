package model

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusDead      JobStatus = "dead"
)

type Job struct {
	ID           uuid.UUID  `db:"id"`
	Type         string     `db:"type"`
	Payload      []byte     `db:"payload"`
	Status       JobStatus  `db:"status"`
	Priority     int        `db:"priority"`
	MaxRetries   int        `db:"max_retries"`
	RetryCount   int        `db:"retry_count"`
	ErrorMessage *string    `db:"error_message"`
	ScheduledAt  time.Time  `db:"scheduled_at"`
	StartedAt    *time.Time `db:"started_at"`
	CompletedAt  *time.Time `db:"completed_at"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
}

type CreateJobRequest struct {
	Type        string         `json:"type"`
	Payload     map[string]any `json:"payload"`
	Priority    int            `json:"priority"`
	MaxRetries  int            `json:"max_retries"`
	ScheduledAt *time.Time     `json:"scheduled_at"`
}

type JobResponse struct {
	ID           uuid.UUID      `json:"id"`
	Type         string         `json:"type"`
	Payload      map[string]any `json:"payload"`
	Status       JobStatus      `json:"status"`
	Priority     int            `json:"priority"`
	MaxRetries   int            `json:"max_retries"`
	RetryCount   int            `json:"retry_count"`
	ErrorMessage *string        `json:"error_message"`
	ScheduledAt  time.Time      `json:"scheduled_at"`
	StartedAt    *time.Time     `json:"started_at"`
	CompletedAt  *time.Time     `json:"completed_at"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (j *Job) ToResponse() (*JobResponse, error) {
	var payload map[string]any

	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	return &JobResponse{
		ID:           j.ID,
		Type:         j.Type,
		Payload:      payload,
		Status:       j.Status,
		Priority:     j.Priority,
		MaxRetries:   j.MaxRetries,
		RetryCount:   j.RetryCount,
		ErrorMessage: j.ErrorMessage,
		ScheduledAt:  j.ScheduledAt,
		StartedAt:    j.StartedAt,
		CompletedAt:  j.CompletedAt,
		CreatedAt:    j.CreatedAt,
		UpdatedAt:    j.UpdatedAt,
	}, nil
}
