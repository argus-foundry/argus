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
	testutil.Equal(t, recentSpawns("claude", "personal", time.Time{}), 0)
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
	testutil.Equal(t, recentSpawns("claude", config.DefaultAccountName, time.Time{}), 1)
	testutil.Equal(t, recentSpawns("codex", "", time.Time{}), 0)
}

// The penalty is a placeholder for usage the next probe has not measured: once
// a reading newer than the spawn lands, the real reading is scored alone.
func TestPenaltyClearsOnFreshReading(t *testing.T) {
	now := resetLedger(t)
	withCachedPct(t, known(40), known(0))
	readingAt := now.Add(-time.Hour)
	restore := usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
		return usagebudget.Reading{Percentage: 38, LastProbedAt: readingAt}, account == "personal"
	})
	defer restore()
	cfg := config.Config{Backends: testBackends(), Accounts: map[string]config.Account{"personal": {ClaudeConfigDir: t.TempDir()}}, BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
		{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
	}}}
	WithReservation(func() Choice { return ResolveChoice(cfg, "claude", "") })
	// Reading is older than the spawn: still penalized, so default wins.
	testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "default")
	// A reading seconds after the spawn cannot include its burn yet: still penalized.
	*now = now.Add(2 * time.Second)
	readingAt = *now
	testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "default")
	// A reading taken SpawnSettle+ after the spawn includes it: penalty gone.
	*now = now.Add(6 * time.Minute)
	readingAt = *now
	testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "personal")
}

func TestPenaltyClearsOnFreshDaemonReading(t *testing.T) {
	now := resetLedger(t)
	at := now.Add(-time.Hour)
	usage := Usage{Claude: map[string]usagebudget.Reading{
		"default":  {Percentage: 40, LastProbedAt: at},
		"personal": {Percentage: 38, LastProbedAt: at},
	}}
	defer SetUsageSource(func() (Usage, error) { return usage, nil })()
	cfg := config.Config{Backends: testBackends(), Accounts: map[string]config.Account{"personal": {ClaudeConfigDir: t.TempDir()}}, BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
		{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
	}}}
	WithReservation(func() Choice { return ResolveChoice(cfg, "claude", "") })
	testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "default")
	*now = now.Add(6 * time.Minute)
	for k, r := range usage.Claude {
		r.LastProbedAt = *now
		usage.Claude[k] = r
	}
	testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "personal")
}

func twoClaudeAccounts(t *testing.T) config.Config {
	t.Helper()
	return config.Config{Backends: testBackends(), Accounts: map[string]config.Account{"personal": {ClaudeConfigDir: t.TempDir()}}, BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
		{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
	}}}
}

// Pins the boundaries of the placeholder: exactly SpawnPenaltyPct points per
// spawn, cleared by a reading taken exactly SpawnSettle after the spawn but not
// one second earlier, and never past SpawnPenaltyWindow without a timestamp.
func TestPenaltyBoundaries(t *testing.T) {
	t.Run("five points per spawn", func(t *testing.T) {
		resetLedger(t)
		cfg := burstCfg(t, 70, 38)
		WithReservation(func() Choice { return ResolveChoice(cfg, "claude", "") })
		c := ResolveChoice(cfg, "claude", "personal")
		testutil.Contains(t, c.Reason, "usage=43.00")
		WithReservation(func() Choice { return ResolveChoice(cfg, "claude", "") })
		testutil.Contains(t, ResolveChoice(cfg, "claude", "personal").Reason, "usage=48.00")
	})
	t.Run("settle boundary", func(t *testing.T) {
		now := resetLedger(t)
		withCachedPct(t, known(40), known(0))
		spawnAt := *now
		readingAt := spawnAt
		t.Cleanup(usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
			return usagebudget.Reading{Percentage: 38, LastProbedAt: readingAt}, account == "personal"
		}))
		cfg := twoClaudeAccounts(t)
		WithReservation(func() Choice { return ResolveChoice(cfg, "claude", "") })
		// Literal spec values, not the constants, so a changed constant fails.
		*now = spawnAt.Add(5 * time.Minute)
		readingAt = spawnAt.Add(5*time.Minute - time.Second)
		testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "default")
		readingAt = spawnAt.Add(5 * time.Minute)
		testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "personal")
	})
	t.Run("window backstop without a timestamp", func(t *testing.T) {
		now := resetLedger(t)
		cfg := burstCfg(t, 40, 38) // bridge readings carry no LastProbedAt
		spawnAt := *now
		WithReservation(func() Choice { return ResolveChoice(cfg, "claude", "") })
		*now = spawnAt.Add(29 * time.Minute)
		testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "default")
		*now = spawnAt.Add(30*time.Minute + time.Second)
		testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "personal")
	})
}

// The Codex reading's own timestamp clears a Codex spawn's placeholder, the
// same as Claude's.
func TestPenaltyClearsOnFreshCodexReading(t *testing.T) {
	now := resetLedger(t)
	at := now.Add(-time.Hour)
	usage := Usage{
		Claude: map[string]usagebudget.Reading{"default": {Percentage: 42, LastProbedAt: at}},
		Codex:  &usagebudget.Reading{Percentage: 40, LastProbedAt: at},
	}
	defer SetUsageSource(func() (Usage, error) { return usage, nil })()
	cfg := config.Config{Backends: testBackends(), BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
		{Backend: "codex", Probe: config.ProbeCodexUsage, ThresholdPct: 80},
	}}}
	testutil.Equal(t, WithReservation(func() Choice { return ResolveChoice(cfg, "", "") }).Backend, "codex")
	testutil.Equal(t, ResolveChoice(cfg, "", "").Backend, "claude") // 45 > 42
	*now = now.Add(SpawnSettle)
	usage.Codex = &usagebudget.Reading{Percentage: 40, LastProbedAt: *now}
	testutil.Equal(t, ResolveChoice(cfg, "", "").Backend, "codex")
}
