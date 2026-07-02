package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/TonyJ275/gotaskq/internal/db"
	"github.com/TonyJ275/gotaskq/internal/model"
)

type Handler struct {
	jobRepo *db.JobRepository
}

func NewHandler(jobRepo *db.JobRepository) *Handler {
	return &Handler{jobRepo: jobRepo}
}

// POST /jobs
func (h *Handler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var req model.CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "job type is required")
		return
	}

	if req.Payload == nil {
		writeError(w, http.StatusBadRequest, "payload is required")
		return
	}

	job, err := h.jobRepo.CreateJob(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create job")
		return
	}

	writeJSON(w, http.StatusCreated, job)
}

// GET /jobs/{id}
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	job, err := h.jobRepo.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	writeJSON(w, http.StatusOK, job)
}

// GET /jobs?status=pending&limit=10&offset=0
func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	status := model.JobStatus(r.URL.Query().Get("status"))
	if status == "" {
		status = model.JobStatusPending
	}

	limit := 10
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	jobs, err := h.jobRepo.ListJobs(r.Context(), status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	writeJSON(w, http.StatusOK, jobs)
}

// DELETE /jobs/{id}
func (h *Handler) CancelJob(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	if err := h.jobRepo.CancelJob(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "job cancelled"})
}

// GET /stats
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.jobRepo.GetStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// helper functions
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
