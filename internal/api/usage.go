package api

import (
	"net/http"
	"time"

	"github.com/drn/argus/internal/backendtier"
	"github.com/drn/argus/internal/uxlog"
)

// providerUsageJSON complements the per-account Claude usage returned by
// /api/accounts. Codex's existing cache measures the default account only.
type providerUsageJSON struct {
	Codex *codexUsageJSON `json:"codex"`
}

type codexUsageJSON struct {
	WeeklyPct      float64 `json:"weekly_pct"`
	WeeklyResetsAt string  `json:"weekly_resets_at,omitempty"`
	LastProbedAt   string  `json:"last_probed_at,omitempty"`
}

// handleUsage serves only the daemon's current cached reading. CachedReading
// enforces Codex's reset/hold validity rules and never starts a provider probe.
func (s *Server) handleUsage(w http.ResponseWriter, _ *http.Request) {
	var out providerUsageJSON
	if reading, ok := backendtier.CachedReading(); ok {
		out.Codex = &codexUsageJSON{WeeklyPct: reading.Percentage}
		if !reading.ResetAt.IsZero() {
			out.Codex.WeeklyResetsAt = reading.ResetAt.UTC().Format(time.RFC3339)
		}
		if !reading.LastProbedAt.IsZero() {
			out.Codex.LastProbedAt = reading.LastProbedAt.UTC().Format(time.RFC3339)
		}
	}
	uxlog.Log("[usage] cached provider snapshot: codex_available=%t", out.Codex != nil)
	writeJSON(w, http.StatusOK, out)
}
