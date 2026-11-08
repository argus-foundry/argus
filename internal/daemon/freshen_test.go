package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drn/argus/internal/backendtier"
	"github.com/drn/argus/internal/testutil"
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
	d.usageBudgetProbe = func(context.Context) error { probed.Add(1); return nil }
	d.codexProbe = func(context.Context) error { return nil }
	var resp backendtier.Usage
	testutil.NoError(t, rpcFor(d).RoutingUsageFresh(&Empty{}, &resp))
	testutil.Equal(t, probed.Load(), int32(1))
}

func TestUsagePollIntervalIsFiveMinutes(t *testing.T) {
	testutil.Equal(t, usageBudgetProbeInterval, 5*time.Minute)
}
