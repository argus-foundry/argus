package backendtier

import (
	"errors"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/usagebudget"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// Usage is one snapshot of every reading tier resolution consumes: Claude per
// account (keyed by account name, "default" for the default account) and the
// Codex weekly reading. An absent entry means "no valid reading".
type Usage struct {
	Claude map[string]usagebudget.Reading
	Codex  *usagebudget.Reading
}

var usageSource struct {
	sync.RWMutex
	read func() (Usage, error)
}

// SetUsageSource redirects tier resolution's usage readings. The usagebudget
// and Codex probe caches are populated only in the daemon process, so a TUI
// process that resolves tiers locally sees every tier as unread; it installs a
// daemon RPC here instead. nil restores local-cache reads. Returns a restore
// func.
func SetUsageSource(read func() (Usage, error)) func() {
	usageSource.Lock()
	previous := usageSource.read
	usageSource.read = read
	usageSource.Unlock()
	return func() {
		usageSource.Lock()
		usageSource.read = previous
		usageSource.Unlock()
	}
}

// readings are the per-resolution reading functions ResolveChoice consults.
type readings struct {
	claude func(account string) (float64, bool)
	codex  func() (float64, bool)
	// claudeAt/codexAt report when the reading was measured and when its
	// window resets (zero = unknown); nil means unknown. Spawns older than the
	// measurement no longer carry a burst penalty, and a remembered reading is
	// not recalled past its reset.
	claudeAt func(account string) readingTimes
	codexAt  func() readingTimes
}

// readingTimes is when a reading was measured and, per quota window, its
// percentage and reset (empty = a single-window reading: the live percentage
// with reset).
type readingTimes struct {
	probed, reset time.Time
	windows       []windowReading
}

// windowReading is one quota window's percentage and reset (zero = unknown).
type windowReading struct {
	pct   float64
	reset time.Time
}

// timesOf keeps every Claude window separately, so a last-known recall can
// score whichever window has not reset yet instead of pairing one window's
// percentage with another's reset.
func timesOf(r usagebudget.Reading) readingTimes {
	t := readingTimes{probed: r.LastProbedAt, reset: r.ResetAt}
	for _, w := range []*usagebudget.Window{r.FiveHour, r.SevenDay} {
		if w != nil {
			t.windows = append(t.windows, windowReading{pct: w.Percentage, reset: w.ResetAt})
		}
	}
	return t
}

// currentReadings returns the reading functions for one resolution: a single
// snapshot from the installed source (one RPC per resolution), or this
// process's own caches when none is installed or the source fails.
func currentReadings() readings {
	usageSource.RLock()
	read := usageSource.read
	usageSource.RUnlock()
	if read != nil {
		u, err := read()
		if err == nil {
			return readings{
				claude: func(account string) (float64, bool) {
					if account == "" {
						account = config.DefaultAccountName
					}
					r, ok := u.Claude[account]
					return r.Percentage, ok
				},
				codex: func() (float64, bool) {
					if u.Codex == nil {
						return 0, false
					}
					return u.Codex.Percentage, true
				},
				claudeAt: func(account string) readingTimes {
					if account == "" {
						account = config.DefaultAccountName
					}
					return timesOf(u.Claude[account])
				},
				codexAt: func() readingTimes {
					if u.Codex == nil {
						return readingTimes{}
					}
					return timesOf(*u.Codex)
				},
			}
		}
		// ErrNoUsageSnapshot is the cached source's routine "not warm yet"; it
		// can fire per role per tick, so it stays silent (the refresh failure
		// itself is logged once per attempt by CachedUsage).
		if !errors.Is(err, ErrNoUsageSnapshot) {
			uxlog.Log("[backendtier] daemon usage read failed, falling back to local caches: %v", err)
		}
	}
	return readings{
		claude: func(account string) (float64, bool) { return claudeUsageForAccount([]string{account})() },
		codex:  func() (float64, bool) { return codexCachedPct() },
		claudeAt: func(account string) readingTimes {
			if account == "" {
				account = config.DefaultAccountName
			}
			r, _ := usagebudget.CachedReadingForAccount(account)
			return timesOf(r)
		},
		codexAt: func() readingTimes {
			r, _ := CachedReading()
			return timesOf(usagebudget.Reading{LastProbedAt: r.LastProbedAt, ResetAt: r.ResetAt})
		},
	}
}

// LocalUsage snapshots this process's caches for the given config's accounts;
// the daemon serves it over RPC to TUI processes.
func LocalUsage(cfg config.Config) Usage {
	u := Usage{Claude: map[string]usagebudget.Reading{}}
	for _, name := range cfg.AccountNamesFor("claude") {
		if r, ok := usagebudget.CachedReadingForAccount(name); ok {
			u.Claude[name] = r
		}
	}
	if r, ok := CachedReading(); ok {
		rr := usagebudget.Reading{Percentage: r.Percentage, ResetAt: r.ResetAt, LastProbedAt: r.LastProbedAt}
		u.Codex = &rr
	}
	return u
}
