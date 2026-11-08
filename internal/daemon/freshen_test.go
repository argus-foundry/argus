package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drn/argus/internal/backendtier"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/usagebudget"
)

func TestFreshenUsageProbesBothAndDebounces(t *testing.T) {
	d, _ := testDaemon(t)
	var claude, codex atomic.Int32
	d.usageBudgetProbe = func(context.Context) error { claude.Add(1); return nil }
	d.codexProbe = func(context.Context) error { codex.Add(1); return nil }

	d.freshenUsage()
	testutil.Equal(t, claude.Load(), int32(1))
	testutil.Equal(t, codex.Load(), int32(1))

	// A burst inside the debounce gap shares the round.
	d.freshenUsage()
	d.freshenUsage()
	testutil.Equal(t, claude.Load(), int32(1))

	d.freshenMu.Lock()
	d.lastFreshen = time.Now().Add(-usageFreshenMinGap - time.Second)
	d.freshenMu.Unlock()
	d.freshenUsage()
	testutil.Equal(t, claude.Load(), int32(2))
	testutil.Equal(t, codex.Load(), int32(2))
}

func TestRoutingUsageFreshRPCProbesFirst(t *testing.T) {
	d, _ := testDaemon(t)
	var probed atomic.Int32
	// The probe publishes a new reading; the RPC must return it, i.e. read
	// usage only after the probe round.
	d.usageBudgetProbe = func(context.Context) error {
		probed.Add(1)
		t.Cleanup(usagebudget.SetBridgeReader(func(account string) (usagebudget.Reading, bool) {
			return usagebudget.Reading{Percentage: 42}, account == config.DefaultAccountName
		}))
		return nil
	}
	d.codexProbe = func(context.Context) error { return nil }
	var resp backendtier.Usage
	testutil.NoError(t, rpcFor(d).RoutingUsageFresh(&Empty{}, &resp))
	testutil.Equal(t, probed.Load(), int32(1))
	testutil.Equal(t, resp.Claude[config.DefaultAccountName].Percentage, 42.0)
}

func TestUsagePollIntervalIsFiveMinutes(t *testing.T) {
	d, _ := testDaemon(t)
	testutil.Equal(t, d.usageProbeInterval(), 5*time.Minute)
	testutil.Equal(t, (&Daemon{}).usageProbeInterval(), 5*time.Minute)
}

// Both pollers tick on the configured cadence (not a literal of their own).
func TestUsagePollersTickOnConfiguredInterval(t *testing.T) {
	d, _ := testDaemon(t)
	d.usageProbeEvery = 5 * time.Millisecond
	var claude, codex atomic.Int32
	d.usageBudgetProbe = func(context.Context) error { claude.Add(1); return nil }
	d.codexProbe = func(context.Context) error { codex.Add(1); return nil }
	stopped := make(chan struct{}, 2)
	go func() { d.runUsageBudgetPoller(); stopped <- struct{}{} }()
	go func() { d.runCodexProbePoller(); stopped <- struct{}{} }()
	deadline := time.Now().Add(5 * time.Second)
	for (claude.Load() < 3 || codex.Load() < 3) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(d.done)
	<-stopped
	<-stopped
	testutil.Equal(t, claude.Load() >= 3, true)
	testutil.Equal(t, codex.Load() >= 3, true)
}

// A stopped daemon's freshener is inert: the process-global hook can be left
// pointing at it when two daemons in one process restore out of order.
func TestFreshenUsageNoopAfterShutdown(t *testing.T) {
	d, _ := testDaemon(t)
	var probed atomic.Int32
	d.usageBudgetProbe = func(context.Context) error { probed.Add(1); return nil }
	d.codexProbe = func(context.Context) error { probed.Add(1); return nil }
	close(d.done)
	d.freshenUsage()
	testutil.Equal(t, probed.Load(), int32(0))
}

// A probe that ignores its context (e.g. blocked on usagebudget's probeMu
// behind an in-flight poll probe) must not hold the spawn past the cap.
func TestFreshenUsageCapIsHard(t *testing.T) {
	d, _ := testDaemon(t)
	orig := usageFreshenTimeout
	usageFreshenTimeout = 50 * time.Millisecond
	t.Cleanup(func() { usageFreshenTimeout = orig })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var rounds atomic.Int32
	codexRan := make(chan struct{}, 4)
	d.usageBudgetProbe = func(context.Context) error { <-release; return nil }
	d.codexProbe = func(context.Context) error { rounds.Add(1); codexRan <- struct{}{}; return nil }

	// Fail (not hang the package) if the cap regresses to an unbounded wait.
	returned := make(chan struct{})
	start := time.Now()
	go func() { d.freshenUsage(); close(returned) }()
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("freshenUsage blocked past its cap")
	}
	testutil.Equal(t, time.Since(start) < 2*time.Second, true) // the 50ms override, not 5s
	select {
	case <-codexRan: // round 1 has started both probes
	case <-time.After(5 * time.Second):
		t.Fatal("round 1 never ran the codex probe")
	}

	// The abandoned round still debounces: a follow-up spawn does not stack a
	// second blocked round.
	d.freshenUsage()
	testutil.Equal(t, rounds.Load(), int32(1))
}

// A spawn queued on freshenMu while the daemon stops must not start a round.
func TestFreshenUsageQueuedSpawnSkipsAfterShutdown(t *testing.T) {
	d, _ := testDaemon(t)
	var probed atomic.Int32
	d.usageBudgetProbe = func(context.Context) error { probed.Add(1); return nil }
	d.codexProbe = func(context.Context) error { probed.Add(1); return nil }
	d.freshenMu.Lock()
	returned := make(chan struct{})
	go func() { d.freshenUsage(); close(returned) }()
	time.Sleep(20 * time.Millisecond) // let it pass the entry check and queue
	close(d.done)
	d.freshenMu.Unlock()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("freshenUsage did not return")
	}
	testutil.Equal(t, probed.Load(), int32(0))
}
