package model

import (
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
