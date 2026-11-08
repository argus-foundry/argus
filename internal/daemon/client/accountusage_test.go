package client

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/drn/argus/internal/backendtier"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/usagebudget"
)

func TestCliUsage(t *testing.T) {
	fd := newFakeDaemon(t)
	reset := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	fd.mu.Lock()
	fd.usage = map[string]usagebudget.Reading{"default": {Percentage: 100, FiveHour: &usagebudget.Window{Percentage: 100, ResetAt: reset}}}
	fd.mu.Unlock()
	c := fakeClient(t, fd)

	got, err := c.AccountUsage()
	testutil.NoError(t, err)
	testutil.Equal(t, got["default"].FiveHour.Percentage, 100.0)
	testutil.True(t, got["default"].FiveHour.ResetAt.Equal(reset))
	testutil.Equal(t, c.AccountUsageSource()(config.Config{})["default"].Percentage, 100.0)
}

func TestCliUsageOld(t *testing.T) {
	fd := newFakeDaemon(t)
	fd.mu.Lock()
	fd.usageMissing = true
	fd.mu.Unlock()
	c := fakeClient(t, fd)

	_, err := c.AccountUsage()
	testutil.Error(t, err)
	testutil.Nil(t, c.AccountUsageSource()(config.Config{}))
}

func TestCliRoutingUsage(t *testing.T) {
	fd := newFakeDaemon(t)
	fd.mu.Lock()
	fd.routing = backendtier.Usage{
		Claude: map[string]usagebudget.Reading{"personal": {Percentage: 70}},
		Codex:  &usagebudget.Reading{Percentage: 12},
	}
	fd.mu.Unlock()
	c := fakeClient(t, fd)

	got, err := c.RoutingUsage()
	testutil.NoError(t, err)
	testutil.Equal(t, got.Claude["personal"].Percentage, 70.0)
	testutil.Equal(t, got.Codex.Percentage, 12.0)

	fd.mu.Lock()
	fd.usageMissing = true
	fd.mu.Unlock()
	_, err = c.RoutingUsage()
	testutil.Error(t, err)
}

func TestCliRouteRedial(t *testing.T) {
	fd := newFakeDaemon(t)
	fd.mu.Lock()
	fd.routing = backendtier.Usage{Claude: map[string]usagebudget.Reading{"personal": {Percentage: 8}}}
	fd.mu.Unlock()
	c := fakeClient(t, fd)

	// Simulate the daemon bounce: this client's connection is dead for good.
	testutil.NoError(t, c.rpc.Close())
	_, origErr := c.RoutingUsage()
	testutil.Error(t, origErr)

	got, err := c.RoutingUsageResilient()
	testutil.NoError(t, err)
	testutil.Equal(t, got.Claude["personal"].Percentage, 8.0)

	// Daemon gone entirely: the ORIGINAL call error surfaces (not the dial
	// error), nothing panics.
	c.sockPath = filepath.Join(t.TempDir(), "gone.sock")
	_, err = c.RoutingUsageResilient()
	testutil.Error(t, err)
	testutil.Equal(t, err.Error(), origErr.Error())

	// A live connection never takes the fallback path: one call, no new dial.
	lfd := newFakeDaemon(t)
	live := fakeClient(t, lfd)
	_, err = live.RoutingUsage() // completes the dial, so it is counted
	testutil.NoError(t, err)
	lfd.mu.Lock()
	acceptsBefore := lfd.accepts
	lfd.mu.Unlock()
	_, err = live.RoutingUsageResilient()
	testutil.NoError(t, err)
	lfd.mu.Lock()
	defer lfd.mu.Unlock()
	testutil.Equal(t, lfd.accepts, acceptsBefore)
	testutil.Equal(t, lfd.routingCalls, 2)
}

func TestCliRouteFresh(t *testing.T) {
	fd := newFakeDaemon(t)
	fd.mu.Lock()
	fd.routing = backendtier.Usage{Claude: map[string]usagebudget.Reading{"personal": {Percentage: 9}}}
	fd.mu.Unlock()
	c := fakeClient(t, fd)
	got, err := c.RoutingUsageFresh()
	testutil.NoError(t, err)
	testutil.Equal(t, got.Claude["personal"].Percentage, 9.0)

	// After a bounce the resilient variant re-dials and still probes.
	testutil.NoError(t, c.rpc.Close())
	got, err = c.RoutingUsageFreshResilient()
	testutil.NoError(t, err)
	testutil.Equal(t, got.Claude["personal"].Percentage, 9.0)
	fd.mu.Lock()
	calls := fd.freshCalls
	fd.mu.Unlock()
	testutil.Equal(t, calls, 2)
}
