package tui

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

// TestUsageRecheckInterval pins the cadence is far slower than the 1s tick —
// the daemon's own probe tickers refresh only every 30 minutes, so polling
// faster buys nothing (fix-backend-routing-semantics).
func TestUsageRecheckInterval(t *testing.T) {
	if usageRecheckInterval < 30*time.Second {
		t.Errorf("usageRecheckInterval = %v; a per-tick BootInfo RPC is not free", usageRecheckInterval)
	}
}

// TestClaimUsageCheck_Gates mirrors TestClaimSkewCheck_Gates: the periodic
// usage poll does not run on every 1s tick, and is inert without a provider
// (--remote mode, or the in-process-runner fallback).
func TestClaimUsageCheck_Gates(t *testing.T) {
	t.Run("no provider ⇒ never claims", func(t *testing.T) {
		app := New(testDB(t), agent.NewRunner(nil), true)
		app.skewProvider = nil
		app.lastUsageCheck = time.Time{}
		_, ok := app.claimUsageCheck()
		testutil.Equal(t, ok, false)
	})

	t.Run("within the interval ⇒ does not claim", func(t *testing.T) {
		app := New(testDB(t), agent.NewRunner(nil), true)
		app.skewProvider = &fakeSkewClient{}
		app.lastUsageCheck = time.Now()
		_, ok := app.claimUsageCheck()
		testutil.Equal(t, ok, false)
	})

	t.Run("past the interval ⇒ claims once, then re-gates", func(t *testing.T) {
		app := New(testDB(t), agent.NewRunner(nil), true)
		app.skewProvider = &fakeSkewClient{}
		app.lastUsageCheck = time.Now().Add(-2 * usageRecheckInterval)

		got, ok := app.claimUsageCheck()
		testutil.Equal(t, ok, true)
		if got == nil {
			t.Fatal("claim returned a nil provider")
		}
		_, ok = app.claimUsageCheck()
		testutil.Equal(t, ok, false)
	})
}

// TestReevaluateUsage_NoClientIsInert pins that the periodic re-poll is a
// no-op without a daemon client: there is no BootInfo to ask for, and it
// must not panic or block.
func TestReevaluateUsage_NoClientIsInert(t *testing.T) {
	app := New(testDB(t), agent.NewRunner(nil), true)
	app.skewProvider = nil
	app.reevaluateUsage()
}

// TestReevaluateUsage_FeedsStatusBar drives the full re-poll path against a
// running event loop: a fresh BootInfo reading must land on the status bar
// without a relaunch.
func TestReevaluateUsage_FeedsStatusBar(t *testing.T) {
	app := New(testDB(t), agent.NewRunner(nil), true)
	_, cleanup := wireApp(t, app)
	defer cleanup()

	app.skewProvider = &fakeSkewClient{resp: daemon.BootInfoResp{
		ClaudeUsagePct:    42,
		ClaudeUsageKnown:  true,
		ClaudeUsageWindow: "five_hour",
		CodexUsagePct:     76,
		CodexUsageKnown:   true,
	}}
	app.lastUsageCheck = time.Now().Add(-2 * usageRecheckInterval)
	app.reevaluateUsage()

	testutil.Contains(t, app.statusbar.UsageSummary(), "cla 42%s · cdx 76%")
}

// TestReevaluateUsage_ShowsNamedClaudeAccounts pins that every configured
// non-default Claude account gets its own entry between cla and cdx
// (show-all-claude-account-usage): a fresh reading renders as a percentage,
// a missing one as "—", in configured (sorted) order.
func TestReevaluateUsage_ShowsNamedClaudeAccounts(t *testing.T) {
	toml := "[accounts.work]\nclaude_config_dir = \"~/work-claude\"\n" +
		"[accounts.personal]\nclaude_config_dir = \"~/personal-claude\"\n"
	app := New(testDBWithConfig(t, toml), agent.NewRunner(nil), true)
	_, cleanup := wireApp(t, app)
	defer cleanup()

	stubTUIAccountUsage(t, map[string]usagebudget.Reading{
		"default":  {Percentage: 99, Window: "five_hour"},
		"personal": {Percentage: 40, Window: "seven_day"},
	})
	app.skewProvider = &fakeSkewClient{resp: daemon.BootInfoResp{
		ClaudeUsagePct:    100,
		ClaudeUsageKnown:  true,
		ClaudeUsageWindow: "seven_day",
		CodexUsagePct:     53,
		CodexUsageKnown:   true,
	}}
	app.lastUsageCheck = time.Now().Add(-2 * usageRecheckInterval)
	app.reevaluateUsage()

	testutil.Equal(t, app.statusbar.UsageSummary(), "cla 100%w · personal 40%w · work — · cdx 53%")
}

// TestClaudeAccountUsage_NoNamedAccounts pins that with only the default
// account configured there are no extra entries at all.
func TestClaudeAccountUsage_NoNamedAccounts(t *testing.T) {
	app := New(testDB(t), agent.NewRunner(nil), true)
	testutil.Nil(t, app.claudeAccountUsage())
}
