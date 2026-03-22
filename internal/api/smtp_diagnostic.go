package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"crontab-reminder/internal/model"
	"crontab-reminder/internal/notifier"
	"crontab-reminder/internal/store"
)

type smtpDiagnosticRequest struct {
	ChannelID  string `json:"channel_id"`
	ProviderID string `json:"provider_id"`
	To         string `json:"to"`
}

type smtpDiagnosticResponse struct {
	Source     string               `json:"source"`
	ChannelID  string               `json:"channel_id,omitempty"`
	ProviderID string               `json:"provider_id,omitempty"`
	Diagnostic model.SMTPDiagnostic `json:"diagnostic"`
}

func (s *Server) smtpDiagnosticHandler(w http.ResponseWriter, r *http.Request) {
	var req smtpDiagnosticRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	req.ChannelID = strings.TrimSpace(req.ChannelID)
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	req.To = strings.TrimSpace(req.To)

	if req.ChannelID != "" && req.ProviderID != "" {
		writeErr(w, http.StatusBadRequest, errors.New("channel_id and provider_id cannot be used together"))
		return
	}

	cfg, source, channelID, providerID, target, err := s.smtpDiagnosticConfig(r, req)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	diag := notifier.ProbeSMTP(r.Context(), cfg, target)
	writeJSON(w, http.StatusOK, smtpDiagnosticResponse{
		Source:     source,
		ChannelID:  channelID,
		ProviderID: providerID,
		Diagnostic: diag,
	})
}

func (s *Server) smtpDiagnosticConfig(r *http.Request, req smtpDiagnosticRequest) (notifier.EmailConfig, string, string, string, string, error) {
	if req.ChannelID != "" {
		ch, err := s.store.GetChannelResource(r.Context(), req.ChannelID)
		if err != nil {
			return notifier.EmailConfig{}, "", "", "", "", err
		}
		if ch.Type != model.ChannelEmail {
			return notifier.EmailConfig{}, "", "", "", "", errors.New("channel must be email")
		}
		target, err := emailTargetFromChannel(*ch)
		if err != nil {
			return notifier.EmailConfig{}, "", "", "", "", err
		}
		if req.To != "" {
			target = req.To
		}
		if ch.ProviderID != "" {
			provider, err := s.store.GetProvider(r.Context(), ch.ProviderID)
			if err != nil {
				return notifier.EmailConfig{}, "", "", "", "", err
			}
			if provider.Type != model.ChannelEmail {
				return notifier.EmailConfig{}, "", "", "", "", errors.New("provider must be email")
			}
			cfg, err := notifier.EmailConfigFromProvider(*provider)
			if err != nil {
				return notifier.EmailConfig{}, "", "", "", "", err
			}
			return cfg, "channel_provider", ch.ID, provider.ID, target, nil
		}
		cfg, err := s.fallbackEmailConfig()
		if err != nil {
			return notifier.EmailConfig{}, "", "", "", "", err
		}
		return cfg, "fallback", ch.ID, "", target, nil
	}

	if req.ProviderID != "" {
		provider, err := s.store.GetProvider(r.Context(), req.ProviderID)
		if err != nil {
			return notifier.EmailConfig{}, "", "", "", "", err
		}
		if provider.Type != model.ChannelEmail {
			return notifier.EmailConfig{}, "", "", "", "", errors.New("provider must be email")
		}
		cfg, err := notifier.EmailConfigFromProvider(*provider)
		if err != nil {
			return notifier.EmailConfig{}, "", "", "", "", err
		}
		return cfg, "provider", "", provider.ID, req.To, nil
	}

	cfg, err := s.fallbackEmailConfig()
	if err != nil {
		return notifier.EmailConfig{}, "", "", "", "", err
	}
	return cfg, "fallback", "", "", req.To, nil
}

func (s *Server) fallbackEmailConfig() (notifier.EmailConfig, error) {
	cfg := notifier.EmailConfig{
		Host: s.stats.SMTPHost,
		Port: s.stats.SMTPPort,
		User: s.stats.SMTPUser,
		Pass: s.stats.SMTPPass,
		From: s.stats.SMTPFrom,
	}
	if cfg.Host == "" {
		return notifier.EmailConfig{}, errors.New("fallback SMTP host is not configured")
	}
	if cfg.Port <= 0 {
		cfg.Port = 587
	}
	return cfg, nil
}

func emailTargetFromChannel(ch model.ChannelResource) (string, error) {
	var cfg struct {
		To string `json:"to"`
	}
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return "", errors.New("email channel config: " + err.Error())
	}
	if strings.TrimSpace(cfg.To) == "" {
		return "", errors.New("email channel missing 'to'")
	}
	return strings.TrimSpace(cfg.To), nil
}
