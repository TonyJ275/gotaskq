package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	db "github.com/TonyJ275/gotaskq/internal/db/mocks"
	"github.com/TonyJ275/gotaskq/internal/model"
)

func newTestHandler() (*Handler, *db.MockJobStore) {
	mock := db.NewMockJobStore()
	handler := NewHandler(mock)
	return handler, mock
}

func makeRequest(t *testing.T, handler http.HandlerFunc, method, url string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req := httptest.NewRequest(method, url, reqBody)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

// ============================================================
// CreateJob Tests
// ============================================================

func TestCreateJob_Success(t *testing.T) {
	handler, mock := newTestHandler()

	body := map[string]any{
		"type":        "send_email",
		"payload":     map[string]any{"to": "test@example.com"},
		"priority":    1,
		"max_retries": 3,
	}

	rr := makeRequest(t, handler.CreateJob, http.MethodPost, "/jobs", body)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rr.Code)
	}

	var resp model.JobResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Type != "send_email" {
		t.Errorf("expected type 'send_email', got '%s'", resp.Type)
	}

	if resp.Status != model.JobStatusPending {
		t.Errorf("expected status 'pending', got '%s'", resp.Status)
	}

	if resp.Priority != 1 {
		t.Errorf("expected priority 1, got %d", resp.Priority)
	}

	if len(mock.Jobs) != 1 {
		t.Errorf("expected 1 job in store, got %d", len(mock.Jobs))
	}
}

func TestCreateJob_MissingType(t *testing.T) {
	handler, _ := newTestHandler()

	body := map[string]any{
		"payload": map[string]any{"to": "test@example.com"},
	}

	rr := makeRequest(t, handler.CreateJob, http.MethodPost, "/jobs", body)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] != "job type is required" {
		t.Errorf("unexpected error message: %s", resp["error"])
	}
}

func TestCreateJob_MissingPayload(t *testing.T) {
	handler, _ := newTestHandler()

	body := map[string]any{
		"type": "send_email",
	}

	rr := makeRequest(t, handler.CreateJob, http.MethodPost, "/jobs", body)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] != "payload is required" {
		t.Errorf("unexpected error message: %s", resp["error"])
	}
}

func TestCreateJob_InvalidJSON(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.CreateJob(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] != "invalid request body" {
		t.Errorf("unexpected error message: %s", resp["error"])
	}
}

func TestCreateJob_DatabaseError(t *testing.T) {
	handler, mock := newTestHandler()
	mock.CreateError = errors.New("database connection lost")

	body := map[string]any{
		"type":    "send_email",
		"payload": map[string]any{"to": "test@example.com"},
	}

	rr := makeRequest(t, handler.CreateJob, http.MethodPost, "/jobs", body)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected error message in response body")
	}
}

func TestCreateJob_DefaultMaxRetries(t *testing.T) {
	handler, mock := newTestHandler()

	body := map[string]any{
		"type":    "send_email",
		"payload": map[string]any{"to": "test@example.com"},
	}

	rr := makeRequest(t, handler.CreateJob, http.MethodPost, "/jobs", body)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rr.Code)
	}

	if len(mock.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(mock.Jobs))
	}

	for _, job := range mock.Jobs {
		if job.MaxRetries != 3 {
			t.Errorf("expected default max_retries 3, got %d", job.MaxRetries)
		}
	}
}

func TestCreateJob_DefaultPriority(t *testing.T) {
	handler, mock := newTestHandler()

	body := map[string]any{
		"type":    "send_email",
		"payload": map[string]any{"to": "test@example.com"},
	}

	rr := makeRequest(t, handler.CreateJob, http.MethodPost, "/jobs", body)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}

	for _, job := range mock.Jobs {
		if job.Priority != 0 {
			t.Errorf("expected default priority 0, got %d", job.Priority)
		}
	}
}

// ============================================================
// GetJob Tests
// ============================================================

func TestGetJob_Success(t *testing.T) {
	handler, mock := newTestHandler()

	existingJob := &model.Job{
		ID:          uuid.New(),
		Type:        "send_email",
		Payload:     []byte(`{"to":"test@example.com"}`),
		Status:      model.JobStatusPending,
		Priority:    1,
		MaxRetries:  3,
		ScheduledAt: time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	mock.Jobs[existingJob.ID] = existingJob

	req := httptest.NewRequest(http.MethodGet, "/jobs/"+existingJob.ID.String(), nil)
	req.SetPathValue("id", existingJob.ID.String())
	rr := httptest.NewRecorder()
	handler.GetJob(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp model.JobResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != existingJob.ID {
		t.Errorf("expected job ID %s, got %s", existingJob.ID, resp.ID)
	}

	if resp.Type != "send_email" {
		t.Errorf("expected type 'send_email', got '%s'", resp.Type)
	}
}

func TestGetJob_InvalidUUID(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/jobs/not-a-uuid", nil)
	req.SetPathValue("id", "not-a-uuid")
	rr := httptest.NewRecorder()
	handler.GetJob(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected error message in response body")
	}
}

func TestGetJob_NotFound(t *testing.T) {
	handler, _ := newTestHandler()

	// Use a random UUID that doesn't exist in mock.Jobs
	randomID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/jobs/"+randomID.String(), nil)
	req.SetPathValue("id", randomID.String())
	rr := httptest.NewRecorder()
	handler.GetJob(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected error message in response body")
	}
}

func TestGetJob_ToResponseError(t *testing.T) {
	handler, mock := newTestHandler()

	job := &model.Job{
		ID:      uuid.New(),
		Type:    "test",
		Payload: []byte("invalid-json"), // this breaks ToResponse
		Status:  model.JobStatusPending,
	}
	mock.Jobs[job.ID] = job

	req := httptest.NewRequest(http.MethodGet, "/jobs/"+job.ID.String(), nil)
	req.SetPathValue("id", job.ID.String())

	rr := httptest.NewRecorder()
	handler.GetJob(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// ============================================================
// GetStats Tests
// ============================================================

func TestGetStats_Success(t *testing.T) {
	handler, mock := newTestHandler()

	mock.Jobs[uuid.New()] = &model.Job{
		ID:     uuid.New(),
		Status: model.JobStatusPending,
	}
	mock.Jobs[uuid.New()] = &model.Job{
		ID:     uuid.New(),
		Status: model.JobStatusCompleted,
	}
	mock.Jobs[uuid.New()] = &model.Job{
		ID:     uuid.New(),
		Status: model.JobStatusCompleted,
	}

	rr := makeRequest(t, handler.GetStats, http.MethodGet, "/stats", nil)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var stats map[string]int
	if err := json.NewDecoder(rr.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if stats["pending"] != 1 {
		t.Errorf("expected 1 pending job, got %d", stats["pending"])
	}

	if stats["completed"] != 2 {
		t.Errorf("expected 2 completed jobs, got %d", stats["completed"])
	}
}

func TestGetStats_DatabaseError(t *testing.T) {
	handler, mock := newTestHandler()
	mock.StatsError = errors.New("database error")

	rr := makeRequest(t, handler.GetStats, http.MethodGet, "/stats", nil)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected error message in response body")
	}
}

// ============================================================
// CancelJob Tests
// ============================================================

func TestCancelJob_Success(t *testing.T) {
	handler, mock := newTestHandler()

	existingJob := &model.Job{
		ID:     uuid.New(),
		Status: model.JobStatusPending,
	}
	mock.Jobs[existingJob.ID] = existingJob

	req := httptest.NewRequest(http.MethodDelete, "/jobs/"+existingJob.ID.String(), nil)
	req.SetPathValue("id", existingJob.ID.String())
	rr := httptest.NewRecorder()
	handler.CancelJob(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	if mock.Jobs[existingJob.ID].Status != model.JobStatusFailed {
		t.Errorf("expected job status to be 'failed' after cancel")
	}
}

func TestCancelJob_InvalidUUID(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest(http.MethodDelete, "/jobs/not-a-uuid", nil)
	req.SetPathValue("id", "not-a-uuid")
	rr := httptest.NewRecorder()
	handler.CancelJob(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected error message in response body")
	}
}

func TestCancelJob_NotPending(t *testing.T) {
	handler, mock := newTestHandler()

	existingJob := &model.Job{
		ID:     uuid.New(),
		Status: model.JobStatusRunning,
	}
	mock.Jobs[existingJob.ID] = existingJob

	req := httptest.NewRequest(http.MethodDelete, "/jobs/"+existingJob.ID.String(), nil)
	req.SetPathValue("id", existingJob.ID.String())
	rr := httptest.NewRecorder()
	handler.CancelJob(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected error message in response body")
	}
}

// ============================================================
// ListJobs Tests
// ============================================================

func TestListJobs_Success(t *testing.T) {
	handler, mock := newTestHandler()

	for i := 0; i < 3; i++ {
		id := uuid.New()
		mock.Jobs[id] = &model.Job{
			ID:          id,
			Status:      model.JobStatusPending,
			Priority:    i,
			ScheduledAt: time.Now(),
		}
	}

	// Add completed job — should not appear in pending list
	id := uuid.New()
	mock.Jobs[id] = &model.Job{
		ID:     id,
		Status: model.JobStatusCompleted,
	}

	req := httptest.NewRequest(http.MethodGet, "/jobs?status=pending&limit=10&offset=0", nil)
	rr := httptest.NewRecorder()
	handler.ListJobs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var jobs []*model.Job
	if err := json.NewDecoder(rr.Body).Decode(&jobs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(jobs) != 3 {
		t.Errorf("expected 3 pending jobs, got %d", len(jobs))
	}

	// Verify sorting — higher priority first
	if len(jobs) > 1 && jobs[0].Priority < jobs[1].Priority {
		t.Errorf("expected higher priority first, got %d before %d",
			jobs[0].Priority, jobs[1].Priority)
	}
}

func TestListJobs_Pagination(t *testing.T) {
	handler, mock := newTestHandler()

	for i := 0; i < 5; i++ {
		id := uuid.New()
		mock.Jobs[id] = &model.Job{
			ID:          id,
			Status:      model.JobStatusPending,
			ScheduledAt: time.Now(),
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/jobs?status=pending&limit=2&offset=0", nil)
	rr := httptest.NewRecorder()
	handler.ListJobs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var jobs []*model.Job
	if err := json.NewDecoder(rr.Body).Decode(&jobs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs with limit=2, got %d", len(jobs))
	}
}

func TestListJobs_DefaultStatusIsPending(t *testing.T) {
	handler, mock := newTestHandler()

	id := uuid.New()
	mock.Jobs[id] = &model.Job{
		ID:          id,
		Status:      model.JobStatusPending,
		ScheduledAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	rr := httptest.NewRecorder()
	handler.ListJobs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var jobs []*model.Job
	if err := json.NewDecoder(rr.Body).Decode(&jobs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(jobs) != 1 {
		t.Errorf("expected 1 pending job, got %d", len(jobs))
	}
}

func TestListJobs_InvalidLimit(t *testing.T) {
	handler, mock := newTestHandler()

	id := uuid.New()
	mock.Jobs[id] = &model.Job{
		ID:          id,
		Status:      model.JobStatusPending,
		ScheduledAt: time.Now(),
	}

	// Invalid limit — should fall back to default of 10
	req := httptest.NewRequest(http.MethodGet, "/jobs?limit=abc", nil)
	rr := httptest.NewRecorder()
	handler.ListJobs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 with invalid limit, got %d", rr.Code)
	}
}

func TestListJobs_InvalidStatus(t *testing.T) {
	handler, _ := newTestHandler()

	// Invalid status — should return empty list, not error
	req := httptest.NewRequest(http.MethodGet, "/jobs?status=invalid", nil)
	rr := httptest.NewRecorder()
	handler.ListJobs(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 with invalid status, got %d", rr.Code)
	}

	var jobs []*model.Job
	if err := json.NewDecoder(rr.Body).Decode(&jobs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs with invalid status, got %d", len(jobs))
	}
}

func TestListJobs_DatabaseError(t *testing.T) {
	handler, mock := newTestHandler()
	mock.ListError = errors.New("database error")

	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	rr := httptest.NewRecorder()
	handler.ListJobs(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["error"] == "" {
		t.Errorf("expected error message in response body")
	}
}
