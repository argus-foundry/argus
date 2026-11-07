package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const maxAccountBody = 8 << 10

// accountWriteMu serializes account and default writes so the validate,
// reference check and store steps of one request cannot interleave with another.
var accountWriteMu sync.Mutex

type accountCreateRequest struct {
	Name            string `json:"name"`
	Label           string `json:"label"`
	ClaudeConfigDir string `json:"claude_config_dir"`
	CodexHome       string `json:"codex_home"`
}

type accountPatchRequest struct {
	Label           *string `json:"label"`
	ClaudeConfigDir *string `json:"claude_config_dir"`
	CodexHome       *string `json:"codex_home"`
}

// decodeStrict reads a size-limited JSON body, rejecting unknown fields and
// trailing data.
func decodeStrict(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAccountBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body", err)
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: trailing data", nil)
		return false
	}
	return true
}

func emitConfigChanged(payload map[string]string) {
	events.Emit(model.EventTypeConfigChanged, "", payload)
}

func (s *Server) accountView(name string) accountJSON {
	cfg := s.db.Config()
	a := accountEntry(cfg, s.db.AccountsFromConfigToml(), name, cfg.ResolveAccount("", ""))
	fillProviderUsage(cfg, &a, agent.AccountUsage(cfg), time.Now())
	return a
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if requireMaster(w, r) {
		return
	}
	var req accountCreateRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	accountWriteMu.Lock()
	defer accountWriteMu.Unlock()

	cfg := s.db.Config()
	req.Name = strings.TrimSpace(req.Name)
	if err := config.ValidateNewAccountName(req.Name, cfg.Accounts); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		writeErr(w, status, err.Error(), nil)
		return
	}
	a := config.Account{
		Label:           strings.TrimSpace(req.Label),
		ClaudeConfigDir: strings.TrimSpace(req.ClaudeConfigDir),
		CodexHome:       strings.TrimSpace(req.CodexHome),
	}
	if err := config.ValidateAccountEdit(cfg.Accounts, req.Name, a); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if err := s.db.SetAccount(req.Name, a); err != nil {
		uxlog.Log("[account] create %s failed: %v", req.Name, err)
		writeErr(w, http.StatusInternalServerError, "failed to save account", err)
		return
	}
	uxlog.Log("[account] created %s", req.Name)
	emitConfigChanged(map[string]string{"resource": "accounts", "name": req.Name})
	writeJSON(w, http.StatusCreated, s.accountView(req.Name))
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	if requireMaster(w, r) {
		return
	}
	name := r.PathValue("name")
	var req accountPatchRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if req.Label == nil && req.ClaudeConfigDir == nil && req.CodexHome == nil {
		writeErr(w, http.StatusBadRequest, "no fields to update: supply label, claude_config_dir or codex_home", nil)
		return
	}
	accountWriteMu.Lock()
	defer accountWriteMu.Unlock()

	cfg := s.db.Config()
	if status, msg := s.accountMutable(cfg, name); status != 0 {
		writeErr(w, status, msg, nil)
		return
	}
	a := cfg.Accounts[name]
	fields := map[string]string{}
	if req.Label != nil {
		a.Label = strings.TrimSpace(*req.Label)
		fields["label"] = a.Label
	}
	if req.ClaudeConfigDir != nil {
		a.ClaudeConfigDir = strings.TrimSpace(*req.ClaudeConfigDir)
		fields["claude_config_dir"] = a.ClaudeConfigDir
	}
	if req.CodexHome != nil {
		a.CodexHome = strings.TrimSpace(*req.CodexHome)
		fields["codex_home"] = a.CodexHome
	}
	if err := config.ValidateAccountEdit(cfg.Accounts, name, a); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if err := s.db.UpdateAccountFields(name, fields); err != nil {
		var invalid *db.AccountValidationError
		if errors.As(err, &invalid) {
			uxlog.Log("[account] update %s rejected: %v", name, err)
			writeErr(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		uxlog.Log("[account] update %s failed: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "failed to save account", err)
		return
	}
	uxlog.Log("[account] updated %s", name)
	emitConfigChanged(map[string]string{"resource": "accounts", "name": name})
	writeJSON(w, http.StatusOK, s.accountView(name))
}

// accountMutable checks account existence and ownership.
func (s *Server) accountMutable(cfg config.Config, name string) (int, string) {
	if name == config.DefaultAccountName {
		return http.StatusBadRequest, "the built-in default account cannot be changed"
	}
	if _, ok := cfg.Accounts[name]; !ok {
		return http.StatusNotFound, "account not found"
	}
	if s.db.AccountsFromConfigToml().Accounts[name] {
		return http.StatusConflict, "account " + name + " is defined in config.toml and is read-only"
	}
	return 0, ""
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if requireMaster(w, r) {
		return
	}
	name := r.PathValue("name")
	accountWriteMu.Lock()
	defer accountWriteMu.Unlock()

	cfg := s.db.Config()
	if status, msg := s.accountMutable(cfg, name); status != 0 {
		writeErr(w, status, msg, nil)
		return
	}
	refs, err := s.accountReferences(cfg, name)
	if err != nil {
		uxlog.Log("[account] references for %s failed: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "failed to read account references", err)
		return
	}
	uxlog.Log("[account] references for %s: tasks=%d projects=%d artifacts=%d tiers=%d default=%t resident=%t", name, len(refs.Tasks), len(refs.Projects), len(refs.ArtifactProjects), len(refs.Tiers), refs.Default, refs.Resident)
	if !refs.empty() {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":      "account " + name + " is still referenced; clear the references first",
			"references": refs,
		})
		return
	}
	if err := s.db.DeleteAccount(name); err != nil {
		uxlog.Log("[account] delete %s failed: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "failed to delete account", err)
		return
	}
	uxlog.Log("[account] deleted %s", name)
	emitConfigChanged(map[string]string{"resource": "accounts", "name": name})
	w.WriteHeader(http.StatusNoContent)
}
