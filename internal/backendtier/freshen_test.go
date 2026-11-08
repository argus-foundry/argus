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
