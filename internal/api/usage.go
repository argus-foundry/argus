package api

import (
	"net/http"
	"time"

	"github.com/drn/argus/internal/backendtier"
	"github.com/drn/argus/internal/usagebudget"
	"github.com/drn/argus/internal/uxlog"
)

var (
	codexUsageSnapshot  = backendtier.CachedReading
	claudeUsageSnapshot = usagebudget.CachedDisplayReadingForAccount
)

// providerUsageJSON serves cache-only quota readings without the sign-in
// subprocess lookups performed by /api/accounts. Codex's cache measures the
// default account only.
type providerUsageJSON struct {
	Claude []claudeUsageAccountJSON `json:"claude"`
	Codex  *codexUsageJSON          `json:"codex"`
}

type claudeUsageAccountJSON struct {
	Name  string            `json:"name"`
	Label string            `json:"label"`
	Usage *accountUsageJSON `json:"usage,omitempty"`
}

type codexUsageJSON struct {
	WeeklyPct      float64 `json:"weekly_pct"`
	WeeklyResetsAt string  `json:"weekly_resets_at,omitempty"`
	LastProbedAt   string  `json:"last_probed_at,omitempty"`
}

// handleUsage serves only the daemon's current cached readings. It never
// starts a provider probe or performs provider sign-in lookups.
func (s *Server) handleUsage(w http.ResponseWriter, _ *http.Request) {
	cfg := s.db.Config()
	now := time.Now()
	out := providerUsageJSON{Claude: make([]claudeUsageAccountJSON, 0)}
	for _, name := range cfg.AccountNamesFor("claude") {
		label := cfg.Accounts[name].Label
		if label == "" {
			label = name
		}
		account := claudeUsageAccountJSON{Name: name, Label: label}
		if reading, ok := claudeUsageSnapshot(name, now); ok {
			account.Usage = usageJSON(reading, true, now)
		}
		out.Claude = append(out.Claude, account)
	}
	if reading, ok := codexUsageSnapshot(); ok {
		out.Codex = &codexUsageJSON{WeeklyPct: reading.Percentage}
		if !reading.ResetAt.IsZero() {
			out.Codex.WeeklyResetsAt = reading.ResetAt.UTC().Format(time.RFC3339)
		}
		if !reading.LastProbedAt.IsZero() {
			out.Codex.LastProbedAt = reading.LastProbedAt.UTC().Format(time.RFC3339)
		}
	}
	uxlog.Log("[usage] cached provider snapshot: claude_accounts=%d codex_available=%t", len(out.Claude), out.Codex != nil)
	writeJSON(w, http.StatusOK, out)
}
