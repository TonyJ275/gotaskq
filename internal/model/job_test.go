package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJob_ToResponse_Success(t *testing.T) {
	// 1. Setup a valid job with proper JSON in the payload
	jobID := uuid.New()
	now := time.Now()

	job := &Job{
		ID:          jobID,
		Type:        "email",
		Payload:     []byte(`{"to": "test@example.com", "subject": "hello"}`),
		Status:      JobStatusPending,
		Priority:    10,
		MaxRetries:  3,
		RetryCount:  0,
		ScheduledAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// 2. Convert to response
	resp, err := job.ToResponse()

	// 3. Assert there is no error
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// 4. Verify the struct mapping worked
	if resp.ID != job.ID {
		t.Errorf("expected ID %v, got %v", job.ID, resp.ID)
	}
	if resp.Type != job.Type {
		t.Errorf("expected Type %v, got %v", job.Type, resp.Type)
	}

	// 5. Verify the JSON unmarshaling worked
	if resp.Payload["to"] != "test@example.com" {
		t.Errorf("expected payload 'to' to be 'test@example.com', got %v", resp.Payload["to"])
	}
}

func TestJob_ToResponse_Error(t *testing.T) {
	// 1. Setup a job with garbage, invalid JSON in the payload
	job := &Job{
		ID:      uuid.New(),
		Type:    "bad_job",
		Payload: []byte(`{this is not valid json}`),
	}

	// 2. Convert to response
	resp, err := job.ToResponse()

	// 3. Assert it throws an error and returns nil
	if err == nil {
		t.Fatalf("expected an error due to invalid json, got none")
	}
	if resp != nil {
		t.Errorf("expected response to be nil on error, got %v", resp)
	}
}
