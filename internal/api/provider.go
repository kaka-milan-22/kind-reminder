package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"crontab-reminder/internal/model"
	"crontab-reminder/internal/store"

	"github.com/go-chi/chi/v5"
)

type createProviderRequest struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request) {
	var req createProviderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Type = strings.TrimSpace(req.Type)

	if req.ID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id is required"))
		return
	}
	if req.Type != string(model.ChannelTelegram) && req.Type != string(model.ChannelEmail) && req.Type != string(model.ChannelWebhook) {
		writeErr(w, http.StatusBadRequest, errors.New("type must be telegram, email or webhook"))
		return
	}
	if len(req.Config) == 0 || string(req.Config) == "null" {
		writeErr(w, http.StatusBadRequest, errors.New("config is required"))
		return
	}

	p := &model.Provider{
		ID:        req.ID,
		Type:      model.ChannelType(req.Type),
		Config:    req.Config,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateProvider(r.Context(), p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, errors.New("provider id already exists"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": p.ID})
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.ListProviders(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	masked := make([]model.Provider, len(providers))
	for i, p := range providers {
		masked[i] = maskProviderConfig(p)
	}
	writeJSON(w, http.StatusOK, masked)
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteProvider(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maskProviderConfig returns a copy with sensitive credential fields redacted.
func maskProviderConfig(p model.Provider) model.Provider {
	var cfg map[string]any
	if err := json.Unmarshal(p.Config, &cfg); err != nil {
		return p
	}
	for _, k := range []string{"bot_token", "pass", "password", "secret", "token"} {
		if _, ok := cfg[k]; ok {
			cfg[k] = "***"
		}
	}
	masked, _ := json.Marshal(cfg)
	p.Config = masked
	return p
}
