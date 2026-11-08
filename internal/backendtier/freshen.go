package backendtier

import (
	"sync"
	"time"

	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/uxlog"
)

// Fresh read before spawn (auto-account-routing, Option A). Readings are polled
// every few minutes, but a spawn must not route on a snapshot that predates a
// burst. CreateAndStart calls FreshenBeforeSpawn before resolving; the daemon
// installs a debounced probe, a TUI process installs a daemon RPC that probes
// and returns the result.
var freshener struct {
	sync.RWMutex
	fn func()
}

// SetFreshener installs the process's fresh-read hook; nil clears it. Returns a
// restore func.
func SetFreshener(fn func()) func() {
	freshener.Lock()
	prev := freshener.fn
	freshener.fn = fn
	freshener.Unlock()
	return func() {
		freshener.Lock()
		freshener.fn = prev
		freshener.Unlock()
	}
}

// FreshenBeforeSpawn refreshes usage readings, if a hook is installed. It may
// block for the probe's duration and must not run on a UI goroutine.
func FreshenBeforeSpawn() {
	freshener.RLock()
	fn := freshener.fn
	freshener.RUnlock()
	if fn != nil {
		fn()
	}
}

// Last-known readings. A tier whose reading is currently unavailable (probe
// failing, daemon unreachable) is scored on the last reading seen for it, up to
// StaleReadingMaxAge and never past the reading's own window reset (after a
// reset the old percentage says nothing), instead of fail-open "unknown",
// which used to resolve to the first tier in list order regardless of how
// depleted it was.
const StaleReadingMaxAge = 6 * time.Hour

var lastKnown struct {
	mu sync.Mutex
	m  map[string]staleReading
}

type staleReading struct {
	pct   float64
	at    time.Time
	reset time.Time // zero = unknown
}

func readingKey(probe, account string) string {
	if account == "" {
		account = config.DefaultAccountName
	}
	return probe + "\x00" + account
}

func rememberReading(probe, account string, pct float64, reset time.Time) {
	lastKnown.mu.Lock()
	defer lastKnown.mu.Unlock()
	if lastKnown.m == nil {
		lastKnown.m = map[string]staleReading{}
	}
	lastKnown.m[readingKey(probe, account)] = staleReading{pct: pct, at: ledgerNow(), reset: reset}
}

func recallReading(probe, account string) (float64, time.Duration, bool) {
	lastKnown.mu.Lock()
	defer lastKnown.mu.Unlock()
	r, ok := lastKnown.m[readingKey(probe, account)]
	if !ok {
		return 0, 0, false
	}
	now := ledgerNow()
	age := now.Sub(r.at)
	if age > StaleReadingMaxAge {
		return 0, 0, false
	}
	if !r.reset.IsZero() && !now.Before(r.reset) {
		uxlog.Log("[routing] last known reading probe=%s account=%q expired at its window reset %s; treating as unknown", probe, account, r.reset.Format(time.RFC3339))
		return 0, 0, false
	}
	uxlog.Log("[routing] reading unavailable probe=%s account=%q; using last known %.2f (%s old)", probe, account, r.pct, age.Round(time.Second))
	return r.pct, age, true
}

// ResetLastKnown forgets remembered readings. Test seam, like ResetSpawnLedger.
func ResetLastKnown() {
	lastKnown.mu.Lock()
	defer lastKnown.mu.Unlock()
	lastKnown.m = nil
}
