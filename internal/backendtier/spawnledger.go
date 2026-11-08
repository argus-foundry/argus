package backendtier

import (
	"sync"
	"time"

	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/uxlog"
)

// Burst spreading (auto-account-routing). Usage readings refresh on a ~30 min
// cadence, so N spawns resolved inside one window would all see the same
// snapshot and land on the same account. The ledger remembers recent spawns
// per (backend, account) in this process and ResolveChoice treats each as
// SpawnPenaltyPct extra usage. The penalty is only a placeholder for usage the
// next probe has not measured yet: routing always scores the REAL reading, and a
// spawn stops counting once a reading taken at least SpawnSettle after it
// arrives (the reading already includes its burn; a fresher-but-too-early one
// does not). SpawnPenaltyWindow is the backstop for readings with
// no timestamp.
const (
	SpawnPenaltyPct    = 5.0
	SpawnPenaltyWindow = 30 * time.Minute
	// SpawnSettle is how long after a spawn a reading must be taken before it
	// is trusted to include that spawn's burn. Live sessions report constantly,
	// so a reading seconds after a spawn cannot reflect it yet.
	SpawnSettle = 5 * time.Minute
)

var ledger struct {
	mu     sync.Mutex
	spawns map[string][]time.Time
	now    func() time.Time
}

// reserveMu serializes WithReservation so concurrent spawns observe each
// other's reservation (resolve + record is one critical section).
var reserveMu sync.Mutex

func ledgerKey(backend, account string) string {
	if account == "" {
		account = config.DefaultAccountName
	}
	return backend + "\x00" + account
}

func ledgerNow() time.Time {
	if ledger.now != nil {
		return ledger.now()
	}
	return time.Now()
}

// recentSpawns counts spawns for the key inside the window and newer than since
// (the account's latest reading time; zero = unknown) by less than SpawnSettle, pruning window-expired
// entries as it goes.
func recentSpawns(backend, account string, since time.Time) int {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	key := ledgerKey(backend, account)
	cutoff := ledgerNow().Add(-SpawnPenaltyWindow)
	kept := ledger.spawns[key][:0]
	for _, t := range ledger.spawns[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(ledger.spawns, key)
		return 0
	}
	ledger.spawns[key] = kept
	n := 0
	for _, t := range kept {
		if since.Before(t.Add(SpawnSettle)) {
			n++
		}
	}
	return n
}

// RecordSpawn notes one spawn on the backend+account.
func RecordSpawn(backend, account string) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.spawns == nil {
		ledger.spawns = map[string][]time.Time{}
	}
	key := ledgerKey(backend, account)
	ledger.spawns[key] = append(ledger.spawns[key], ledgerNow())
}

// WithReservation runs resolve and records the resulting spawn atomically with
// respect to other WithReservation callers. Plain ResolveChoice stays pure so
// previews and display lookups never reserve.
func WithReservation(resolve func() Choice) Choice {
	reserveMu.Lock()
	defer reserveMu.Unlock()
	c := resolve()
	if c.Backend != "" {
		RecordSpawn(c.Backend, c.Account)
		uxlog.Log("[routing] reserved spawn backend=%q account=%q", c.Backend, c.Account)
	}
	return c
}

// penalized returns pct plus the recent-spawn penalty and the spawn count.
func penalized(backend, account string, pct float64, readingAt time.Time) (float64, int) {
	n := recentSpawns(backend, account, readingAt)
	return pct + float64(n)*SpawnPenaltyPct, n
}

// ResetSpawnLedger forgets every recorded spawn. Test seam: the ledger is
// process-global, so tests that assert routing outcomes clear it first.
func ResetSpawnLedger() {
	ledger.mu.Lock()
	ledger.spawns = nil
	ledger.mu.Unlock()
	ResetLastKnown()
}
