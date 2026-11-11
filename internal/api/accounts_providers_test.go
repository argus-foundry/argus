package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/claudeaccount"
	"github.com/argus-foundry/argus/internal/testutil"
)

func providerRollout(t *testing.T, home string, shortPct float64, plan string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "11", "07")
	testutil.NoError(t, os.MkdirAll(dir, 0o755))
	line := fmt.Sprintf(`{"timestamp":"2026-11-07T11:00:00Z","payload":{"rate_limits":{"plan_type":%q,"primary":{"used_percent":%g,"window_minutes":300,"resets_at":%d},"secondary":{"used_percent":40,"window_minutes":10080,"resets_at":%d}}}}`,
		plan, shortPct, time.Now().Add(2*time.Hour).Unix(), time.Now().Add(72*time.Hour).Unix())
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "rollout-x.jsonl"), []byte(line+"\n"), 0o644))
}

func TestHandleListAccounts_Providers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubAuth(t, func(context.Context, string) (claudeaccount.Status, error) {
		return claudeaccount.Status{LoggedIn: true, Email: "a@example.com", Plan: "max", Org: "Acme"}, nil
	})
	stubCodexLogin(t, func(context.Context, string) (bool, error) { return true, nil })

	codexHome := t.TempDir()
	providerRollout(t, codexHome, 25, "plus")
	secret := "sk-test-secret-should-never-appear"
	testutil.NoError(t, os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"token":"`+secret+`"}`), 0o600))
	other := t.TempDir()

	toml := fmt.Sprintf(`
[accounts.mixed]
claude_config_dir = "/tmp/argus-test-mixed"
codex_home = %q

[accounts.claudeonly]
claude_config_dir = "/tmp/argus-test-claudeonly"

[accounts.codexonly]
codex_home = %q
`, codexHome, other)
	srv, _ := accountsServer(t, toml)

	var got []accountJSON
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/accounts", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.False(t, strings.Contains(w.Body.String(), secret))
	by := map[string]accountJSON{}
	for _, a := range got {
		by[a.Name] = a
	}

	t.Run("codex short and weekly windows", func(t *testing.T) {
		c := by["mixed"].Providers.Codex
		testutil.True(t, c.Provider != nil && *c.Provider == "openai")
		testutil.True(t, c.Supported)
		testutil.True(t, c.LoggedIn != nil && *c.LoggedIn)
		testutil.True(t, c.Identity.Plan != nil && *c.Identity.Plan == "plus")
		testutil.True(t, c.Identity.Email == nil && c.Identity.Organization == nil)
		testutil.True(t, c.Usage != nil)
		testutil.Equal(t, len(c.Usage.Windows), 2)
		testutil.Equal(t, c.Usage.Windows[0].Kind, "five_hour")
		testutil.Equal(t, c.Usage.Windows[0].PercentUsed, 25.0)
		testutil.Equal(t, c.Usage.Windows[1].Kind, "seven_day")
	})

	t.Run("claude identity under claude provider only", func(t *testing.T) {
		c := by["mixed"].Providers.Claude
		testutil.True(t, c.Provider != nil && *c.Provider == "anthropic")
		testutil.True(t, c.Identity.Email != nil && *c.Identity.Email == "a@example.com")
		testutil.True(t, c.Identity.Plan != nil && *c.Identity.Plan == "max")
		testutil.True(t, by["mixed"].Providers.Codex.Identity.Email == nil)
	})

	t.Run("unsupported provider is null, not zero", func(t *testing.T) {
		c := by["claudeonly"].Providers.Codex
		testutil.False(t, c.Supported)
		testutil.True(t, c.Provider == nil && c.LoggedIn == nil && c.Usage == nil)
		testutil.True(t, by["codexonly"].Providers.Claude.Provider == nil)
	})

	t.Run("account without rollouts has null usage", func(t *testing.T) {
		testutil.True(t, by["codexonly"].Providers.Codex.Usage == nil)
	})

	t.Run("wire shape keeps explicit nulls", func(t *testing.T) {
		var raw []struct {
			Providers map[string]map[string]json.RawMessage `json:"providers"`
		}
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
		for _, a := range raw {
			cod := a.Providers["codex"]
			testutil.Contains(t, string(cod["identity"]), `"email":null`)
			if _, ok := cod["usage"]; !ok {
				t.Fatal("usage key missing")
			}
		}
	})

	t.Run("requires authentication", func(t *testing.T) {
		h := authMiddleware(srv.token, srv.db, nil, srv.routes(), "/")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/accounts", nil))
		testutil.Equal(t, w.Code, http.StatusUnauthorized)
	})
}

func TestHandleListAccounts_ProviderRerouted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubAuth(t, func(context.Context, string) (claudeaccount.Status, error) { return claudeaccount.Status{}, nil })
	stubCodexLogin(t, func(context.Context, string) (bool, error) { return false, nil })
	srv, _ := accountsServer(t, `
[backends.claude]
command = "claude"
[backends.claude.env_vars]
ANTHROPIC_BASE_URL = "HERA_X"
`)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/accounts", ""))
	var got []accountJSON
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.True(t, got[0].Providers.Claude.Supported)
	testutil.True(t, got[0].Providers.Claude.Provider == nil)
	testutil.True(t, got[0].Providers.Codex.Provider != nil)
}
