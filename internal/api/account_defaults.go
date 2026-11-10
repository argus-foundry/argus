package api

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// accountDefaultJSON describes the stored selection and resolved default chain.
type accountDefaultJSON struct {
	Explicit  *string `json:"explicit"`
	Effective string  `json:"effective"`
	Source    string  `json:"source"`
	Editable  bool    `json:"editable"`
}

func explicitPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func globalDefaultView(cfg config.Config, src config.AccountSources) accountDefaultJSON {
	d := accountDefaultJSON{Explicit: explicitPtr(cfg.DefaultAccount), Effective: cfg.ResolveAccount("", ""), Editable: !src.Default}
	switch {
	case src.Default:
		d.Source = "config_file"
	case cfg.DefaultAccount != "":
		d.Source = "database"
	default:
		d.Source = "builtin"
	}
	return d
}

func projectDefaultView(cfg config.Config, src config.AccountSources, project string) accountDefaultJSON {
	d := accountDefaultJSON{
		Explicit:  explicitPtr(cfg.ProjectAccounts[project]),
		Effective: cfg.ResolveAccount("", project),
		Editable:  !src.Projects[project],
	}
	switch {
	case src.Projects[project]:
		d.Source = "config_file"
	case d.Explicit != nil:
		d.Source = "database"
	default:
		d.Source = "inherited"
	}
	return d
}

func (s *Server) handleGetAccountDefaults(w http.ResponseWriter, r *http.Request) {
	cfg := s.db.Config()
	src := s.db.AccountsFromConfigToml()
	projects := make(map[string]accountDefaultJSON, len(cfg.Projects))
	for p := range cfg.Projects {
		projects[p] = projectDefaultView(cfg, src, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"global":   globalDefaultView(cfg, src),
		"projects": projects,
	})
}

type accountDefaultRequest struct {
	Account json.RawMessage `json:"account"`
}

// parseAccountDefault returns the requested account name, or "" for an
// explicit null (clear). ok=false means an error response was written.
func parseAccountDefault(w http.ResponseWriter, r *http.Request) (account string, ok bool) {
	var req accountDefaultRequest
	if !decodeStrict(w, r, &req) {
		return "", false
	}
	raw := bytes.TrimSpace(req.Account)
	if len(raw) == 0 {
		writeErr(w, http.StatusBadRequest, `missing "account": use null to clear the default`, nil)
		return "", false
	}
	if string(raw) == "null" {
		return "", true
	}
	if err := json.Unmarshal(raw, &account); err != nil || account == "" {
		writeErr(w, http.StatusBadRequest, `"account" must be an account name or null`, nil)
		return "", false
	}
	return account, true
}

func validateAccountDefault(w http.ResponseWriter, cfg config.Config, account string) bool {
	if account == "" {
		return true
	}
	if _, known := cfg.Accounts[account]; account != config.DefaultAccountName && !known {
		writeErr(w, http.StatusBadRequest, "unknown account "+account, nil)
		return false
	}
	if err := cfg.ValidateAccount(account); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), nil)
		return false
	}
	return true
}

func (s *Server) handlePutAccountDefault(w http.ResponseWriter, r *http.Request) {
	account, ok := parseAccountDefault(w, r)
	if !ok {
		return
	}
	accountWriteMu.Lock()
	defer accountWriteMu.Unlock()

	cfg := s.db.Config()
	if !validateAccountDefault(w, cfg, account) {
		return
	}
	src := s.db.AccountsFromConfigToml()
	if src.Default {
		writeErr(w, http.StatusConflict, "default_account is defined in config.toml and is read-only", nil)
		return
	}
	if err := s.db.SetDefaultAccount(account); err != nil {
		uxlog.Log("[account] set global default failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "failed to save default account", err)
		return
	}
	uxlog.Log("[account] global default set to %q", account)
	emitConfigChanged(map[string]string{"resource": "account_defaults"})
	cfg = s.db.Config()
	writeJSON(w, http.StatusOK, globalDefaultView(cfg, s.db.AccountsFromConfigToml()))
}

func (s *Server) handlePutProjectAccount(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("name")
	account, ok := parseAccountDefault(w, r)
	if !ok {
		return
	}
	accountWriteMu.Lock()
	defer accountWriteMu.Unlock()

	cfg := s.db.Config()
	if _, ok := cfg.Projects[project]; !ok {
		writeErr(w, http.StatusNotFound, "project not found", nil)
		return
	}
	if !validateAccountDefault(w, cfg, account) {
		return
	}
	if s.db.AccountsFromConfigToml().Projects[project] {
		writeErr(w, http.StatusConflict, "project "+project+" account is defined in config.toml and is read-only", nil)
		return
	}
	if err := s.db.SetProjectAccount(project, account); err != nil {
		uxlog.Log("[account] set project %s default failed: %v", project, err)
		writeErr(w, http.StatusInternalServerError, "failed to save project account", err)
		return
	}
	uxlog.Log("[account] project %s default set to %q", project, account)
	emitConfigChanged(map[string]string{"resource": "account_defaults", "project": project})
	cfg = s.db.Config()
	writeJSON(w, http.StatusOK, projectDefaultView(cfg, s.db.AccountsFromConfigToml(), project))
}
