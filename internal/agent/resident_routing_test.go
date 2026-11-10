package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
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
	restore := usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
		pct := 95.0
		if account == "work" {
			pct = 10
		}
		return usagebudget.Reading{Percentage: pct}, true
	})
	defer restore()
	cfg := config.DefaultConfig()
	cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: t.TempDir()}}
	cfg.BackendRouting.Strategy = config.StrategyHeadroom
	cfg.BackendRouting.Tiers = []config.BackendTier{{Backend: "claude", Account: "default", Probe: config.ProbeClaudeUsage, ThresholdPct: 90}}
	choice, err := ResolveResidentWorker(cfg, "proj", "", "")
	testutil.NoError(t, err)
	testutil.Equal(t, choice.Account, "work")
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
