package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/claudeaccount"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/usagebudget"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// claudeAuthStatus is the sign-in lookup seam; tests replace it.
var claudeAuthStatus = claudeaccount.CachedAuthStatus

// codexLoginStatus is the Codex sign-in lookup seam; tests replace it.
var codexLoginStatus = agent.CodexLoginStatus

const claudeAuthLookupTimeout = 3 * time.Second

// accountSupportsJSON reports which tools an account can run.
type accountSupportsJSON struct {
	Claude bool `json:"claude"`
	Codex  bool `json:"codex"`
}

// accountJSON is one entry of GET /api/accounts. It carries identity labels
// only, never credentials; logged_in/email/org/plan describe the Claude
// sign-in and stay empty for an account without a Claude config dir.
// codex_logged_in is the Codex sign-in, absent when unknown or unsupported.
type accountJSON struct {
	Name            string               `json:"name"`
	Label           string               `json:"label"`
	ClaudeConfigDir string               `json:"claude_config_dir"`
	CodexHome       string               `json:"codex_home"`
	Supports        accountSupportsJSON  `json:"supports"`
	IsDefault       bool                 `json:"is_default"`
	LoggedIn        bool                 `json:"logged_in"`
	Email           string               `json:"email,omitempty"`
	Org             string               `json:"org,omitempty"`
	Plan            string               `json:"plan,omitempty"`
	CodexLoggedIn   *bool                `json:"codex_logged_in,omitempty"`
	Source          accountSourceJSON    `json:"source"`
	Editable        bool                 `json:"editable"`
	Usage           *accountUsageJSON    `json:"usage,omitempty"`
	Providers       accountProvidersJSON `json:"providers"`
}

type accountProvidersJSON struct {
	Claude providerJSON `json:"claude"`
	Codex  providerJSON `json:"codex"`
}

type providerIdentityJSON struct {
	Email        *string `json:"email"`
	Plan         *string `json:"plan"`
	Organization *string `json:"organization"`
}

// providerJSON is one tool's execution provider, identity and quota for an
// account. Unknown values are null, never zero or a guessed identity.
type providerJSON struct {
	Provider  *string                    `json:"provider"`
	Supported bool                       `json:"supported"`
	LoggedIn  *bool                      `json:"logged_in"`
	Identity  providerIdentityJSON       `json:"identity"`
	Usage     *backendtier.ProviderUsage `json:"usage"`
}

var reroutingEnv = map[string][]string{
	"claude": {"ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"},
	"codex":  {"OPENAI_BASE_URL"},
}

// executionProvider names the verified provider, or nil when unsupported or
// when the backend's env_vars re-route the tool elsewhere.
func executionProvider(cfg config.Config, tool, provider string, supported bool) *string {
	if !supported {
		return nil
	}
	for _, k := range reroutingEnv[tool] {
		if _, ok := cfg.Backends[tool].EnvVars[k]; ok {
			return nil
		}
	}
	return &provider
}

// fillProviderUsage attaches account-scoped quota and the Codex plan; it reads
// only cached readings and rollout rate_limits, never credentials.
func fillProviderUsage(cfg config.Config, a *accountJSON, claude map[string]usagebudget.Reading, now time.Time) {
	if r, ok := claude[a.Name]; ok {
		a.Providers.Claude.Usage = backendtier.ClaudeProviderUsage(r, true, now)
	}
	if home, _, err := cfg.CodexHome(a.Name); err == nil {
		snap := backendtier.CodexHomeUsage(home, now)
		a.Providers.Codex.Usage = snap.Usage
		a.Providers.Codex.Identity.Plan = strPtr(snap.Plan)
	}
}

// accountSourceJSON names where each editable account field comes from:
// "builtin" (the default account), "database" (Settings/REST edited) or
// "config_file" (config.toml, which replaces the whole account and is read-only).
type accountSourceJSON struct {
	Label           string `json:"label"`
	ClaudeConfigDir string `json:"claude_config_dir"`
	CodexHome       string `json:"codex_home"`
}

func sourceFor(name string, sources config.AccountSources) (accountSourceJSON, bool) {
	src := "database"
	switch {
	case name == config.DefaultAccountName:
		src = "builtin"
	case sources.Accounts[name]:
		src = "config_file"
	}
	return accountSourceJSON{Label: src, ClaudeConfigDir: src, CodexHome: src}, src == "database"
}

// accountEntry is the credential-free, lookup-free view of one valid account.
func accountEntry(cfg config.Config, sources config.AccountSources, name, defName string) accountJSON {
	label := cfg.Accounts[name].Label
	if label == "" {
		label = name
	}
	claudeDir, _, claudeErr := cfg.ClaudeConfigDir(name)
	codexHome, _, codexErr := cfg.CodexHome(name)
	a := accountJSON{
		Name:            name,
		Label:           label,
		ClaudeConfigDir: claudeDir,
		CodexHome:       codexHome,
		Supports:        accountSupportsJSON{Claude: claudeErr == nil, Codex: codexErr == nil},
		IsDefault:       name == defName,
	}
	a.Source, a.Editable = sourceFor(name, sources)
	a.Providers.Claude = providerJSON{Provider: executionProvider(cfg, "claude", "anthropic", claudeErr == nil), Supported: claudeErr == nil}
	a.Providers.Codex = providerJSON{Provider: executionProvider(cfg, "codex", "openai", codexErr == nil), Supported: codexErr == nil}
	return a
}

// accountUsageJSON is an account's currently valid Claude usage (bridge
// reading, else background probe); a window with no valid reading is omitted.
type accountUsageJSON struct {
	FiveHourPct      *float64 `json:"five_hour_pct,omitempty"`
	FiveHourResetsAt string   `json:"five_hour_resets_at,omitempty"`
	WeeklyPct        *float64 `json:"weekly_pct,omitempty"`
	WeeklyResetsAt   string   `json:"weekly_resets_at,omitempty"`
}

func usageJSON(r usagebudget.Reading, ok bool, now time.Time) *accountUsageJSON {
	if !ok {
		return nil
	}
	five, week := usagebudget.LiveWindows(r, now)
	if five == nil && week == nil {
		return nil
	}
	out := &accountUsageJSON{}
	if five != nil {
		pct := five.Percentage
		out.FiveHourPct, out.FiveHourResetsAt = &pct, five.ResetAt.UTC().Format(time.RFC3339)
	}
	if week != nil {
		pct := week.Percentage
		out.WeeklyPct, out.WeeklyResetsAt = &pct, week.ResetAt.UTC().Format(time.RFC3339)
	}
	return out
}

// handleListAccounts lists the built-in default account followed by the
// configured ones. is_default marks the account a new task would resolve to
// with no explicit choice: for ?project=<name> that project's default, else the
// global default. Claude sign-in lookups run in parallel under a short
// timeout and degrade to logged_in:false.
func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	cfg := s.db.Config()
	names := cfg.AccountNames()
	defName := cfg.ResolveAccount("", r.URL.Query().Get("project"))
	sources := s.db.AccountsFromConfigToml()
	out := make([]accountJSON, len(names))

	ctx, cancel := context.WithTimeout(r.Context(), claudeAuthLookupTimeout)
	defer cancel()

	var wg sync.WaitGroup
	for i, name := range names {
		if err := cfg.ValidateAccount(name); err != nil {
			uxlog.Log("[account] list: skipping %q: %v", name, err)
			continue
		}
		out[i] = accountEntry(cfg, sources, name, defName)
		claudeDir, _, claudeErr := cfg.ClaudeConfigDir(name)
		codexHome, _, codexErr := cfg.CodexHome(name)
		if codexErr == nil {
			source := ""
			if name != config.DefaultAccountName {
				source = codexHome
			}
			wg.Add(1)
			go func(i int, source string) {
				defer wg.Done()
				ok, err := codexLoginStatus(ctx, source)
				if err != nil {
					uxlog.Log("[account] list: codex sign-in lookup failed for %q: %v", out[i].Name, err)
					return
				}
				out[i].CodexLoggedIn = &ok
				out[i].Providers.Codex.LoggedIn = &ok
			}(i, source)
		}
		if claudeErr != nil {
			continue
		}
		wg.Add(1)
		go func(i int, dir string) {
			defer wg.Done()
			st, err := claudeAuthStatus(ctx, dir)
			if err != nil {
				uxlog.Log("[account] list: sign-in lookup failed for %q: %v", out[i].Name, err)
				return
			}
			out[i].LoggedIn, out[i].Email, out[i].Org, out[i].Plan = st.LoggedIn, st.Email, st.Org, st.Plan
			c := &out[i].Providers.Claude
			loggedIn := st.LoggedIn
			c.LoggedIn = &loggedIn
			c.Identity = providerIdentityJSON{Email: strPtr(st.Email), Plan: strPtr(st.Plan), Organization: strPtr(st.Org)}
		}(i, claudeDir)
	}
	wg.Wait()

	usage := agent.AccountUsage(cfg)
	now := time.Now()
	res := make([]accountJSON, 0, len(out))
	for _, a := range out {
		if a.Name == "" {
			continue
		}
		r, ok := usage[a.Name]
		a.Usage = usageJSON(r, ok, now)
		fillProviderUsage(cfg, &a, usage, now)
		res = append(res, a)
	}
	writeJSON(w, http.StatusOK, res)
}
