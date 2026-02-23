package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"crontab-reminder/internal/model"
	"crontab-reminder/internal/store"

	"github.com/go-chi/chi/v5"
)

type createChannelRequest struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	ProviderID string          `json:"provider_id"`
	Config     json.RawMessage `json:"config"`
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var req createChannelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Type = strings.TrimSpace(req.Type)
	req.ProviderID = strings.TrimSpace(req.ProviderID)

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

	// Validate provider exists if specified.
	if req.ProviderID != "" {
		prov, err := s.store.GetProvider(r.Context(), req.ProviderID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("provider %q not found", req.ProviderID))
				return
			}
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if string(prov.Type) != req.Type {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("provider type %q does not match channel type %q", prov.Type, req.Type))
			return
		}
	}

	ch := &model.ChannelResource{
		ID:         req.ID,
		Type:       model.ChannelType(req.Type),
		ProviderID: req.ProviderID,
		Config:     req.Config,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.store.CreateChannelResource(r.Context(), ch); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, errors.New("channel id already exists"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": ch.ID})
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.store.ListChannelResources(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	masked := make([]model.ChannelResource, len(channels))
	for i, ch := range channels {
		masked[i] = maskChannelConfig(ch)
	}
	writeJSON(w, http.StatusOK, masked)
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteChannelResource(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maskChannelConfig returns a copy of the channel with sensitive config fields redacted.
func maskChannelConfig(ch model.ChannelResource) model.ChannelResource {
	var cfg map[string]any
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return ch
	}
	for _, k := range []string{"bot_token", "pass", "password", "smtp_pass", "secret", "token"} {
		if _, ok := cfg[k]; ok {
			cfg[k] = "***"
		}
	}
	masked, _ := json.Marshal(cfg)
	ch.Config = masked
	return ch
}
