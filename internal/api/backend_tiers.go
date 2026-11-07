package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const maxBackendTiers = 32

var tierCodexReading = backendtier.CachedReading

type backendTierJSON struct {
	Backend      string                 `json:"backend"`
	Account      string                 `json:"account"`
	Probe        string                 `json:"probe"`
	ThresholdPct int                    `json:"threshold_pct"`
	Usage        *backendtier.TierUsage `json:"usage"`
}

type backendTiersJSON struct {
	Source   string            `json:"source"`
	Editable bool              `json:"editable"`
	Revision string            `json:"revision"`
	Tiers    []backendTierJSON `json:"tiers"`
}

type backendTierReadingJSON struct {
	Index int                    `json:"index"`
	Usage *backendtier.TierUsage `json:"usage"`
}

type backendTierRequest struct {
	Backend      string `json:"backend"`
	Account      string `json:"account"`
	Probe        string `json:"probe"`
	ThresholdPct int    `json:"threshold_pct"`
}

type backendTiersPutRequest struct {
	Tiers *[]backendTierRequest `json:"tiers"`
}

func (s *Server) backendTiersView(withUsage bool) backendTiersJSON {
	cfg := s.db.Config()
	fromTOML := s.db.BackendTiersFromConfigToml()
	out := backendTiersJSON{
		Source:   "database",
		Editable: !fromTOML,
		Revision: backendtier.TierRevision(cfg.BackendRouting.Tiers),
		Tiers:    []backendTierJSON{},
	}
	if fromTOML {
		out.Source = "config_file"
	}
	claude := agent.AccountUsage(cfg)
	now := time.Now()
	for _, t := range cfg.BackendRouting.Tiers {
		e := backendTierJSON{Backend: t.Backend, Account: t.Account, Probe: t.Probe, ThresholdPct: t.ThresholdPct}
		if withUsage {
			e.Usage = backendtier.ProjectTierUsage(cfg, t, claude, tierCodexReading, now)
		}
		out.Tiers = append(out.Tiers, e)
	}
	return out
}

func (s *Server) handleGetBackendTiers(w http.ResponseWriter, r *http.Request) {
	view := s.backendTiersView(true)
	w.Header().Set("ETag", `"`+view.Revision+`"`)
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleGetBackendTierUsage(w http.ResponseWriter, r *http.Request) {
	view := s.backendTiersView(true)
	readings := make([]backendTierReadingJSON, len(view.Tiers))
	for i, t := range view.Tiers {
		readings[i] = backendTierReadingJSON{Index: i, Usage: t.Usage}
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": view.Revision, "readings": readings})
}

func ifMatchRevision(r *http.Request) (string, bool) {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	if v == "" {
		return "", false
	}
	v = strings.TrimPrefix(v, "W/")
	return strings.Trim(v, `"`), true
}

func validateBackendTiers(cfg config.Config, in []backendTierRequest) ([]config.BackendTier, string) {
	if len(in) > maxBackendTiers {
		return nil, "too many tiers"
	}
	out := make([]config.BackendTier, 0, len(in))
	for i, t := range in {
		t.Backend = strings.TrimSpace(t.Backend)
		t.Account = strings.TrimSpace(t.Account)
		b, ok := cfg.Backends[t.Backend]
		if !ok {
			return nil, tierErr(i, "unknown backend "+t.Backend)
		}
		switch t.Probe {
		case config.ProbeClaudeUsage, config.ProbeCodexUsage, config.ProbeNone:
		default:
			return nil, tierErr(i, "unknown probe "+t.Probe)
		}
		if t.ThresholdPct < 0 || t.ThresholdPct > 100 {
			return nil, tierErr(i, "threshold_pct must be between 0 and 100")
		}
		if t.Account != "" {
			if err := cfg.ValidateAccount(t.Account); err != nil {
				return nil, tierErr(i, err.Error())
			}
			if !cfg.AccountSupports(t.Account, b.Command) {
				return nil, tierErr(i, "account "+t.Account+" does not support backend "+t.Backend)
			}
		}
		out = append(out, config.BackendTier{Backend: t.Backend, Account: t.Account, Probe: t.Probe, ThresholdPct: t.ThresholdPct})
	}
	return out, ""
}

func tierErr(i int, msg string) string {
	return "tiers[" + strconv.Itoa(i) + "]: " + msg
}

func (s *Server) handlePutBackendTiers(w http.ResponseWriter, r *http.Request) {
	var req backendTiersPutRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if req.Tiers == nil {
		writeErr(w, http.StatusBadRequest, `missing "tiers": use [] to clear the list`, nil)
		return
	}
	accountWriteMu.Lock()
	defer accountWriteMu.Unlock()

	cfg := s.db.Config()
	if s.db.BackendTiersFromConfigToml() {
		writeErr(w, http.StatusConflict, "backend tiers are defined in config.toml and are read-only", nil)
		return
	}
	rev, ok := ifMatchRevision(r)
	if !ok {
		writeErr(w, http.StatusPreconditionRequired, "If-Match revision from GET /api/backend-tiers is required", nil)
		return
	}
	if rev != backendtier.TierRevision(cfg.BackendRouting.Tiers) {
		writeErr(w, http.StatusPreconditionFailed, "backend tiers changed; reload and retry", nil)
		return
	}
	tiers, msg := validateBackendTiers(cfg, *req.Tiers)
	if msg != "" {
		writeErr(w, http.StatusBadRequest, msg, nil)
		return
	}
	if err := s.db.SetBackendTiers(tiers); err != nil {
		uxlog.Log("[backend-tiers] save failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "failed to save backend tiers", err)
		return
	}
	uxlog.Log("[backend-tiers] saved %d tiers", len(tiers))
	emitConfigChanged(map[string]string{"resource": "backend_tiers"})
	view := s.backendTiersView(true)
	w.Header().Set("ETag", `"`+view.Revision+`"`)
	writeJSON(w, http.StatusOK, view)
}
