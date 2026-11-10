package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/backendtier"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

func TestResidentWorkerRouting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: t.TempDir()}, "codex-only": {CodexHome: t.TempDir()}}
	cfg.Resident.WorkerAccounts = map[string]bool{"default": false}
	testutil.DeepEqual(t, ResidentWorkerAccounts(cfg), []string{"default", "work"})
	for _, tc := range []struct{ name, backend, account, want string }{
		{"auto", "", "", "work"}, {"explicit", "claude", "work", "work"},
		{"codex", "codex", "", ""}, {"disabled", "claude", "default", ""}, {"unsupported", "", "codex-only", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			choice, err := ResolveResidentWorker(cfg, "proj", tc.backend, tc.account)
			if tc.want == "" {
				testutil.True(t, err != nil)
				if tc.name == "disabled" || tc.name == "unsupported" {
					// spec: rejection names the enabled accounts
					testutil.Contains(t, err.Error(), "work")
				}
			} else {
				testutil.NoError(t, err)
				testutil.Equal(t, choice.Account, tc.want)
				testutil.Equal(t, choice.Backend, "claude")
			}
		})
	}
	cfg.Resident.WorkerAccounts["work"] = false
	_, err := ResolveResidentWorker(cfg, "proj", "", "")
	testutil.Contains(t, err.Error(), "no account is enabled")
	cfg.Accounts["new"] = config.Account{ClaudeConfigDir: t.TempDir()}
	choice, err := ResolveResidentWorker(cfg, "proj", "", "")
	testutil.NoError(t, err)
	testutil.Equal(t, choice.Account, "new")
}

func TestResidentRoutingHeadroom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Other tests' spawns charge the process-wide burst ledger; start clean.
	backendtier.ResetSpawnLedger()
	t.Cleanup(backendtier.ResetSpawnLedger)
	used := map[string]float64{"default": 95, "work": 10}
	restore := usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
		if account == "" { // the default account is read under the empty key
			account = "default"
		}
		return usagebudget.Reading{Percentage: used[account]}, true
	})
	defer restore()
	cfg := config.DefaultConfig()
	cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: t.TempDir()}}
	cfg.BackendRouting.Strategy = config.StrategyHeadroom
	// Threshold 99 keeps both accounts eligible, so only headroom ranking decides.
	cfg.BackendRouting.Tiers = []config.BackendTier{{Backend: "claude", Account: "default", Probe: config.ProbeClaudeUsage, ThresholdPct: 99}}
	choice, err := ResolveResidentWorker(cfg, "proj", "", "")
	testutil.NoError(t, err)
	testutil.Equal(t, choice.Account, "work")
	used["default"], used["work"] = 10, 95
	choice, err = ResolveResidentWorker(cfg, "proj", "", "")
	testutil.NoError(t, err)
	testutil.Equal(t, choice.Account, "default")
}

func TestResidentRoutingFiltersBeforeRanking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Backends["special"] = config.Backend{Command: "claude --model sonnet"}
	cfg.Projects = map[string]config.Project{"proj": {Backend: "special"}}
	cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: t.TempDir()}}
	cfg.Resident.WorkerAccounts = map[string]bool{"unknown-9a": true, "work": true, "default": false}
	cfg.BackendRouting.Tiers = []config.BackendTier{{Backend: "codex", Probe: config.ProbeNone}, {Backend: "special", Account: "work", Probe: config.ProbeNone}, {Backend: "claude", Probe: config.ProbeNone}}
	choice, err := ResolveResidentWorker(cfg, "proj", "", "")
	testutil.NoError(t, err)
	testutil.Equal(t, choice.Backend, "special")
	testutil.Equal(t, choice.Account, "work")
	_, err = ResolveResidentWorker(cfg, "proj", "missing", "")
	testutil.Contains(t, err.Error(), "ineligible")
	cfg.Resident.WorkerAccounts["default"] = true
	choice, err = ResolveResidentWorker(cfg, "proj", "special", "default")
	testutil.NoError(t, err)
	testutil.Equal(t, choice.Account, "default")
	testutil.Equal(t, choice.Backend, "special")
	cfg.Backends = map[string]config.Backend{"codex": {Command: "codex"}}
	testutil.DeepEqual(t, ResidentWorkerAccounts(cfg), []string{"default", "work"})
	_, err = ResolveResidentWorker(cfg, "proj", "", "")
	testutil.Contains(t, err.Error(), "no eligible resident-worker backend/account pair")
}

func TestResidentRoutingOrderedOverBudgetKeepsEligibleSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	restore := usagebudget.SetBridgeReader(func(string) (usagebudget.Reading, bool) { return usagebudget.Reading{Percentage: 100}, true })
	defer restore()
	cfg := config.DefaultConfig()
	cfg.BackendRouting.Strategy = config.StrategyOrdered
	cfg.BackendRouting.Tiers = []config.BackendTier{{Backend: "codex", Probe: config.ProbeNone}, {Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 90}}
	choice, err := ResolveResidentWorker(cfg, "proj", "", "")
	testutil.NoError(t, err)
	testutil.Equal(t, choice.Backend, "claude")
}

// A deleted then re-created account is enabled for resident workers again:
// the stale "disabled" toggle is dropped with the delete, matching the spec's
// "a new account added later is enabled".
func TestResidentWorkerRoutingRecreatedAccountEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	testutil.NoError(t, d.SetAccount("alt", config.Account{ClaudeConfigDir: t.TempDir()}))
	testutil.NoError(t, d.SetConfigValue("resident.worker_accounts", `{"alt":false}`))
	_, err = ResolveResidentWorker(d.Config(), "proj", "claude", "alt")
	testutil.True(t, err != nil)

	testutil.NoError(t, d.DeleteAccount("alt"))
	testutil.NoError(t, d.SetAccount("alt", config.Account{ClaudeConfigDir: t.TempDir()}))
	choice, err := ResolveResidentWorker(d.Config(), "proj", "claude", "alt")
	testutil.NoError(t, err)
	testutil.Equal(t, choice.Account, "alt")
}
