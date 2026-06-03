package api

import (
	_ "embed"
	"net/http"
)

// dashboardHTML is the self-contained operations dashboard, baked into the
// binary at build time. It has no external dependencies and makes only
// same-origin API calls (so no CORS), carrying the bearer token the user
// pastes in (kept in the browser's localStorage).
//
//go:embed dashboard.html
var dashboardHTML []byte

// uiHandler serves the dashboard. It is intentionally unauthenticated — the
// page itself contains no secrets; every API call it makes is bearer-gated.
func (s *Server) uiHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(dashboardHTML)
}
