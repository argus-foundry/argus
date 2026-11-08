package backendtier

import (
	"testing"
	"time"

	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/usagebudget"
)

func TestFreshenBeforeSpawnRunsInstalledHook(t *testing.T) {
	FreshenBeforeSpawn() // no hook: no-op
	calls := 0
	restore := SetFreshener(func() { calls++ })
	FreshenBeforeSpawn()
	FreshenBeforeSpawn()
	testutil.Equal(t, calls, 2)
	restore()
	FreshenBeforeSpawn()
	testutil.Equal(t, calls, 2)
}

// An unreadable tier is scored on its last known reading, not fail-open
// "unknown", which used to pick the first tier however depleted it was.
func TestUnavailableReadingUsesLastKnownNotFirstTier(t *testing.T) {
	now := resetLedger(t)
	claude := known(78) // default is depleted
	withCachedPct(t, func() (float64, bool) { return claude() }, known(0))
	personalKnown := true
	restore := usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
		return usagebudget.Reading{Percentage: 8}, account == "personal" && personalKnown
	})
	defer restore()
	cfg := config.Config{Backends: testBackends(), Accounts: map[string]config.Account{"personal": {ClaudeConfigDir: t.TempDir()}}, BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
		{Backend: "claude", Account: "personal", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
	}}}
	testutil.Equal(t, ResolveChoice(cfg, "claude", "").Account, "personal")

	// Probes start failing: every reading unknown. Last known keeps personal.
	claude = unknown()
	personalKnown = false
	*now = now.Add(time.Hour)
	c := ResolveChoice(cfg, "claude", "")
	testutil.Equal(t, c.Account, "personal")
	testutil.Contains(t, c.Reason, "known=true")

	// Too old to trust: back to fail-open unknown.
	*now = now.Add(StaleReadingMaxAge + time.Minute)
	c = ResolveChoice(cfg, "claude", "")
	testutil.Contains(t, c.Reason, "known=false")
}

func TestCachedUsageStorePublishesSnapshot(t *testing.T) {
	c, _ := newTestCached(func() (Usage, error) { return Usage{}, ErrNoUsageSnapshot })
	_, err := c.Read()
	testutil.Error(t, err)
	waitIdle(t, c)
	c.Store(Usage{Claude: map[string]usagebudget.Reading{"personal": {Percentage: 3}}})
	u, err := c.Read()
	testutil.NoError(t, err)
	testutil.Equal(t, u.Claude["personal"].Percentage, 3.0)
}

// A remembered reading is no longer "last known" once its own window has
// reset: after a Codex weekly reset there is no fresh rollout until a session
// runs, and recalling the pre-reset 100% would shun Codex for up to 6h.
func TestLastKnownReadingExpiresAtItsReset(t *testing.T) {
	now := resetLedger(t)
	ResetLastKnown()
	t.Cleanup(ResetLastKnown)
	usage := Usage{
		Claude: map[string]usagebudget.Reading{"default": {Percentage: 50}},
		Codex:  &usagebudget.Reading{Percentage: 100, ResetAt: now.Add(time.Hour)},
	}
	defer SetUsageSource(func() (Usage, error) { return usage, nil })()
	cfg := config.Config{Backends: testBackends(), BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
		{Backend: "codex", Probe: config.ProbeCodexUsage, ThresholdPct: 80},
	}}}
	testutil.Equal(t, ResolveChoice(cfg, "", "").Backend, "claude")

	// Before the reset an unavailable Codex reading still recalls 100%.
	usage.Codex = nil
	usage.Claude["default"] = usagebudget.Reading{Percentage: 85}
	*now = now.Add(30 * time.Minute)
	testutil.Equal(t, ResolveChoice(cfg, "", "").Backend, "claude")

	// Past its reset the 100% is meaningless: Codex is unknown, which beats an
	// over-threshold Claude.
	*now = now.Add(time.Hour)
	c := ResolveChoice(cfg, "", "")
	testutil.Equal(t, c.Backend, "codex")
	testutil.Contains(t, c.Reason, "known=false")
}

// The last-known fallback holds right up to StaleReadingMaxAge, not just for
// an hour.
func TestLastKnownReadingAgeBoundary(t *testing.T) {
	now := resetLedger(t)
	pct, ok := 78.0, true
	withCachedPct(t, func() (float64, bool) { return pct, ok }, known(0))
	t.Cleanup(usagebudget.SetBridgeReader(func(string) (usagebudget.Reading, bool) { return usagebudget.Reading{}, false }))
	cfg := config.Config{Backends: testBackends(), BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
	}}}
	start := *now
	ResolveChoice(cfg, "claude", "")
	ok = false
	*now = start.Add(6*time.Hour - time.Minute) // spec literal, not the constant
	testutil.Contains(t, ResolveChoice(cfg, "claude", "").Reason, "known=true")
	*now = start.Add(6*time.Hour + time.Second)
	testutil.Contains(t, ResolveChoice(cfg, "claude", "").Reason, "known=false")
}

// Claude readings carry two windows; each is remembered with its own reset.
// Recall scores the fullest window that has not reset: a refilled 5-hour
// window stops dragging the account down, while a weekly window still deep in
// use keeps it known, until that window resets too.
func TestLastKnownClaudeReadingRecallsPerWindow(t *testing.T) {
	now := resetLedger(t)
	t.Cleanup(ResetLastKnown)
	start := *now
	r := usagebudget.Reading{
		Percentage: 95, ResetAt: start.Add(30 * time.Minute),
		FiveHour: &usagebudget.Window{Percentage: 95, ResetAt: start.Add(30 * time.Minute)},
		SevenDay: &usagebudget.Window{Percentage: 85, ResetAt: start.Add(3 * time.Hour)},
	}
	usage := Usage{Claude: map[string]usagebudget.Reading{"default": r}}
	defer SetUsageSource(func() (Usage, error) { return usage, nil })()
	cfg := config.Config{Backends: testBackends(), BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
	}}}
	ResolveChoice(cfg, "claude", "")
	usage.Claude = map[string]usagebudget.Reading{}
	*now = start.Add(29 * time.Minute) // both windows live: the fuller one
	testutil.Contains(t, ResolveChoice(cfg, "claude", "").Reason, "known=true usage=95.00")
	*now = start.Add(time.Hour) // 5-hour reset: the weekly one
	testutil.Contains(t, ResolveChoice(cfg, "claude", "").Reason, "known=true usage=85.00")
	*now = start.Add(3 * time.Hour) // every window reset (inside the 6h age cap)
	testutil.Contains(t, ResolveChoice(cfg, "claude", "").Reason, "known=false")
}

// Local-cache path: a Claude bridge reading's windows are remembered the same
// way, each expiring exactly at its reset instant.
func TestLastKnownLocalClaudeReadingExpiresAtReset(t *testing.T) {
	now := resetLedger(t)
	start := *now
	pct, ok := 95.0, true
	withCachedPct(t, func() (float64, bool) { return pct, ok }, unknown())
	t.Cleanup(usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
		return usagebudget.Reading{
			Percentage: 95, ResetAt: start.Add(30 * time.Minute),
			FiveHour: &usagebudget.Window{Percentage: 95, ResetAt: start.Add(30 * time.Minute)},
			SevenDay: &usagebudget.Window{Percentage: 60, ResetAt: start.Add(time.Hour)},
		}, ok
	}))
	cfg := config.Config{Backends: testBackends(), BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "claude", Probe: config.ProbeClaudeUsage, ThresholdPct: 80},
	}}}
	ResolveChoice(cfg, "claude", "")
	ok = false
	*now = start.Add(45 * time.Minute)
	testutil.Contains(t, ResolveChoice(cfg, "claude", "").Reason, "known=true usage=60.00")
	*now = start.Add(time.Hour - time.Second)
	testutil.Contains(t, ResolveChoice(cfg, "claude", "").Reason, "known=true")
	*now = start.Add(time.Hour)
	testutil.Contains(t, ResolveChoice(cfg, "claude", "").Reason, "known=false")
}

// Local-cache path for Codex: the cached reading's reset is remembered too.
func TestLastKnownLocalCodexReadingExpiresAtReset(t *testing.T) {
	resetCodexState(t)
	now := resetLedger(t)
	start := *now
	codexNowFunc = func() time.Time { return *now }
	storeCodexReading(Reading{Percentage: 100, ResetAt: start.Add(time.Hour), LastProbedAt: start})
	withCachedPct(t, unknown(), func() (float64, bool) { return CachedCodexPct() })
	cfg := config.Config{Backends: testBackends(), BackendRouting: config.BackendRoutingConfig{Strategy: config.StrategyHeadroom, Tiers: []config.BackendTier{
		{Backend: "codex", Probe: config.ProbeCodexUsage, ThresholdPct: 80},
	}}}
	testutil.Contains(t, ResolveChoice(cfg, "codex", "").Reason, "known=true")
	*now = start.Add(time.Hour) // cache invalid from here; recall must refuse too
	testutil.Contains(t, ResolveChoice(cfg, "codex", "").Reason, "known=false")
}
