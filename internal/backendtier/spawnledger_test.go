package backendtier

import (
	"testing"
	"time"

	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/usagebudget"
)

func resetLedger(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	ledger.mu.Lock()
	ledger.spawns, ledger.now = nil, func() time.Time { return now }
	ledger.mu.Unlock()
	t.Cleanup(func() {
		ledger.mu.Lock()
		ledger.spawns, ledger.now = nil, nil
		ledger.mu.Unlock()
	})
	return &now
}

func burstCfg(t *testing.T, defaultPct, personalPct float64) config.Config {
	t.Helper()
	withCachedPct(t, known(defaultPct), known(0))
	restore := usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
		return usagebudget.Reading{Percentage: personalPct}, account == "personal"
	})
	t.Cleanup(restore)
	return config.Config{Backends: testBackends(), Accounts: map[string]config.Account{"personal": {ClaudeConfigDir: t.TempDir()}}, BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
		{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
	}}}
}

func TestBurstSpreadsAcrossAccounts(t *testing.T) {
	resetLedger(t)
	cfg := burstCfg(t, 40, 38)
	counts := map[string]int{}
	for i := 0; i < 6; i++ {
		c := WithReservation(func() Choice { return ResolveChoice(cfg, "claude", "") })
		counts[c.Account]++
	}
	// Without the penalty all six would land on personal (38 < 40).
	testutil.Equal(t, counts["personal"], 3)
	testutil.Equal(t, counts["default"], 3)
}

func TestSpawnPenaltyExpires(t *testing.T) {
	now := resetLedger(t)
	cfg := burstCfg(t, 40, 38)
	WithReservation(func() Choice { return ResolveChoice(cfg, "claude", "") })
	testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "default")
	*now = now.Add(SpawnPenaltyWindow + time.Second)
	testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "personal")
}

func TestResolveChoiceIsPure(t *testing.T) {
	resetLedger(t)
	cfg := burstCfg(t, 40, 38)
	for i := 0; i < 5; i++ {
		testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "personal")
	}
	testutil.Equal(t, recentSpawns("claude", "personal"), 0)
}

func TestUnreadTiersRotateByFewestSpawns(t *testing.T) {
	resetLedger(t)
	withCachedPct(t, unknown(), unknown())
	cfg := config.Config{Backends: testBackends(), Accounts: map[string]config.Account{"personal": {ClaudeConfigDir: t.TempDir()}}, BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
		{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
	}}}
	restore := usagebudget.SetBridgeReader(func(string) (usagebudget.Reading, bool) { return usagebudget.Reading{}, false })
	defer restore()
	var got []string
	for i := 0; i < 4; i++ {
		got = append(got, WithReservation(func() Choice { return ResolveChoice(cfg, "claude", "") }).Account)
	}
	testutil.DeepEqual(t, got, []string{"default", "personal", "default", "personal"})
}

func TestLedgerKeyNormalizesDefault(t *testing.T) {
	resetLedger(t)
	RecordSpawn("claude", "")
	testutil.Equal(t, recentSpawns("claude", config.DefaultAccountName), 1)
	testutil.Equal(t, recentSpawns("codex", ""), 0)
}
