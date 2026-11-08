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
// StaleReadingMaxAge, scoring the fullest of its windows that has not reset
// (a reset window's old percentage says nothing), instead of fail-open "unknown",
// which used to resolve to the first tier in list order regardless of how
// depleted it was.
const StaleReadingMaxAge = 6 * time.Hour

var lastKnown struct {
	mu sync.Mutex
	m  map[string]staleReading
}

type staleReading struct {
	at      time.Time
	windows []windowReading // each window's percentage and reset (zero = unknown)
}

func readingKey(probe, account string) string {
	if account == "" {
		account = config.DefaultAccountName
	}
	return probe + "\x00" + account
}

// rememberReading stores the live reading per window. A reading without
// windows (Codex, or a stubbed percentage) is one window: the live pct with
// the reading's reset.
func rememberReading(probe, account string, pct float64, t readingTimes) {
	windows := t.windows
	if len(windows) == 0 {
		windows = []windowReading{{pct: pct, reset: t.reset}}
	}
	lastKnown.mu.Lock()
	defer lastKnown.mu.Unlock()
	if lastKnown.m == nil {
		lastKnown.m = map[string]staleReading{}
	}
	lastKnown.m[readingKey(probe, account)] = staleReading{at: ledgerNow(), windows: windows}
}

// recallReading returns the fullest remembered window that has not reset yet,
// if the reading was last available within StaleReadingMaxAge. Every window
// reset means the old percentages say nothing: unknown.
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
	pct, live := 0.0, false
	for _, w := range r.windows {
		if (w.reset.IsZero() || now.Before(w.reset)) && (!live || w.pct > pct) {
			pct, live = w.pct, true
		}
	}
	if !live {
		uxlog.Log("[routing] last known reading probe=%s account=%q: every window has reset since; treating as unknown", probe, account)
		return 0, 0, false
	}
	uxlog.Log("[routing] reading unavailable probe=%s account=%q; using last known %.2f (%s old)", probe, account, pct, age.Round(time.Second))
	return pct, age, true
}

// ResetLastKnown forgets remembered readings. Test seam, like ResetSpawnLedger.
func ResetLastKnown() {
	lastKnown.mu.Lock()
	defer lastKnown.mu.Unlock()
	lastKnown.m = nil
}
