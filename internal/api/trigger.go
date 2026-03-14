package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"crontab-reminder/internal/model"
	"crontab-reminder/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type triggerRequest struct {
	Override map[string]map[string]any `json:"override"`
}

type triggerResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
	Trigger     string `json:"trigger"`
}

func (s *Server) triggerJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	// Check idempotency key
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey != "" {
		existing, err := s.store.FindExecutionByIdempotencyKey(r.Context(), jobID, idempotencyKey)
		if err == nil {
			// Already exists — return idempotent response
			code := http.StatusCreated
			if existing.Status == model.ExecutionSuccess || existing.Status == model.ExecutionFailed {
				code = http.StatusOK
			}
			writeJSON(w, code, triggerResponse{
				ExecutionID: existing.ID,
				Status:      string(existing.Status),
				Trigger:     model.TriggerTypeManual,
			})
			return
		}
	}

	// Get job
	job, err := s.store.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Parse request body (max 64KB)
	var req triggerRequest
	if r.ContentLength != 0 {
		limited := io.LimitReader(r.Body, 64*1024)
		dec := json.NewDecoder(limited)
		if err := dec.Decode(&req); err != nil && err != io.EOF {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}

	// Insert execution (DB lock — if already running, unique index fires → 409)
	execID := uuid.NewString()
	triggeredBy := "api"
	created, err := s.store.InsertRunningExecution(r.Context(), execID, jobID, nil, model.TriggerTypeManual, triggeredBy, idempotencyKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !created {
		writeErr(w, http.StatusConflict, errors.New("job already running"))
		return
	}

	// Launch execution in background (always uses background context so HTTP disconnect doesn't cancel it)
	go s.runner.RunExecution(context.Background(), job, execID, nil, req.Override)

	// Check ?wait=true
	waitParam := r.URL.Query().Get("wait")
	if waitParam == "true" {
		timeout := 30 * time.Second
		if ts := r.URL.Query().Get("timeout"); ts != "" {
			if secs, err := strconv.Atoi(ts); err == nil && secs > 0 {
				timeout = time.Duration(secs) * time.Second
			}
		}

		waitCtx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				exec, err := s.store.GetExecution(r.Context(), execID)
				if err != nil {
					writeErr(w, http.StatusNotFound, err)
					return
				}
				if exec.Status == model.ExecutionSuccess || exec.Status == model.ExecutionFailed {
					writeJSON(w, http.StatusOK, triggerResponse{
						ExecutionID: execID,
						Status:      string(exec.Status),
						Trigger:     model.TriggerTypeManual,
					})
					return
				}
			case <-waitCtx.Done():
				writeJSON(w, http.StatusAccepted, triggerResponse{
					ExecutionID: execID,
					Status:      string(model.ExecutionRunning),
					Trigger:     model.TriggerTypeManual,
				})
				return
			}
		}
	}

	writeJSON(w, http.StatusCreated, triggerResponse{
		ExecutionID: execID,
		Status:      string(model.ExecutionRunning),
		Trigger:     model.TriggerTypeManual,
	})
}

func (s *Server) getExecutionHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	exec, err := s.store.GetExecution(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, exec)
}
