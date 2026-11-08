package daemon

import (
	"log/slog"
	"sync"
	"syscall"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/uxlog"
)

// browserReapInterval is the orphaned-browser sweeper's tick. A steady-state
// tick is one in-process process-table sysctl, so a short interval is cheap.
// A var only so the loop test can shorten it.
var browserReapInterval = 5 * time.Second

// browserReapKillGrace is the SIGTERM->SIGKILL wait: long enough for Chrome to
// shut down cleanly (its dock icon goes away on SIGTERM), short enough that a
// wedged browser does not linger.
const browserReapKillGrace = 5 * time.Second

type browserIdentity struct {
	pid   int32
	start int64
}

// browserReaper closes orphaned automation browsers launched under argus
// agents (reap-orphaned-playwright-browsers). It scans every tick, even while
// the switch is off, because the scan is also what records ownership of
// still-parented browsers (the only ownership signal for Codex agents). A
// failed scan or unreadable config closes nothing. tick is driven by one
// loop; escalation goroutines share only the pending set, guarded by mu.
type browserReaper struct {
	scan      func() ([]agent.OrphanBrowser, error)
	signal    func(agent.OrphanBrowser, syscall.Signal) (bool, error)
	enabled   func() (bool, error) // browsers.close_orphaned; error = config unreadable
	cancel    <-chan struct{}      // daemon shutdown
	killGrace time.Duration

	mu      sync.Mutex
	pending map[browserIdentity]bool // SIGTERMed, escalation in flight
	wg      sync.WaitGroup

	lastScanErr string
	lastCfgErr  string
	wasOff      bool
}

func newBrowserReaper(scan func() ([]agent.OrphanBrowser, error), enabled func() (bool, error)) *browserReaper {
	return &browserReaper{
		scan: scan, signal: agent.SignalOrphanBrowser, enabled: enabled,
		killGrace: browserReapKillGrace, pending: map[browserIdentity]bool{},
	}
}

// switchOn reads browsers.close_orphaned. An unreadable config.toml means the
// setting is unknown, not "on": nothing is closed.
func (r *browserReaper) switchOn() bool {
	on, err := r.enabled()
	if err != nil {
		if msg := err.Error(); msg != r.lastCfgErr {
			r.lastCfgErr = msg
			slog.Warn("browser reaper: config.toml unreadable, closing nothing", "err", err)
			uxlog.Log("[browserreap] config unreadable, closing nothing: %v", err)
		}
		return false
	}
	r.lastCfgErr = ""
	if !on && !r.wasOff {
		slog.Info("browser reaper: disabled by browsers.close_orphaned")
		uxlog.Log("[browserreap] disabled by browsers.close_orphaned")
	}
	r.wasOff = !on
	return on
}

// tick runs one pass and returns how many browsers it sent SIGTERM.
func (r *browserReaper) tick() int {
	orphans, err := r.scan()
	if err != nil {
		if msg := err.Error(); msg != r.lastScanErr {
			r.lastScanErr = msg
			slog.Warn("browser reaper: process scan failed, closing nothing", "err", err)
			uxlog.Log("[browserreap] scan failed, closing nothing: %v", err)
		}
		return 0
	}
	r.lastScanErr = ""
	if len(orphans) == 0 || !r.switchOn() {
		return 0
	}
	closed := 0
	for _, b := range orphans {
		id := browserIdentity{b.PID, b.Start}
		r.mu.Lock()
		inFlight := r.pending[id]
		r.mu.Unlock()
		if inFlight {
			continue
		}
		sent, err := r.signal(b, syscall.SIGTERM)
		if err != nil {
			slog.Warn("browser reaper: SIGTERM failed", "pid", b.PID, "task", b.TaskID, "err", err)
			uxlog.Log("[browserreap] SIGTERM pid=%d task=%s failed: %v", b.PID, b.TaskID, err)
			continue
		}
		if !sent {
			continue // gone or reused since the scan
		}
		closed++
		slog.Info("browser reaper: closing orphaned browser", "pid", b.PID, "task", b.TaskID)
		uxlog.Log("[browserreap] SIGTERM orphaned browser pid=%d task=%s", b.PID, b.TaskID)
		r.mu.Lock()
		r.pending[id] = true
		r.mu.Unlock()
		r.wg.Add(1)
		go r.escalate(b, id)
	}
	return closed
}

// escalate SIGKILLs b if the same process instance is still orphaned after
// the grace period. Daemon shutdown cancels it.
func (r *browserReaper) escalate(b agent.OrphanBrowser, id browserIdentity) {
	defer r.wg.Done()
	defer func() {
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
	}()
	timer := time.NewTimer(r.killGrace)
	defer timer.Stop()
	select {
	case <-r.cancel:
		return
	case <-timer.C:
	}
	sent, err := r.signal(b, syscall.SIGKILL)
	switch {
	case err != nil:
		slog.Warn("browser reaper: SIGKILL failed", "pid", b.PID, "err", err)
		uxlog.Log("[browserreap] SIGKILL pid=%d failed: %v", b.PID, err)
	case sent:
		slog.Info("browser reaper: SIGKILLed browser that ignored SIGTERM", "pid", b.PID, "grace", r.killGrace)
		uxlog.Log("[browserreap] SIGKILL pid=%d after %s", b.PID, r.killGrace)
	}
}

// runBrowserReaper is the sweeper's d.done-gated loop.
func (d *Daemon) runBrowserReaper() {
	scan := d.browserScan
	if scan == nil {
		scan = agent.NewBrowserScanner().Scan
	}
	r := newBrowserReaper(scan, func() (bool, error) {
		on := d.db.Config().Browsers.CloseOrphaned
		return on, d.db.ConfigFileErr() // read AFTER Config(), which re-parses
	})
	r.cancel = d.done
	defer r.wg.Wait()
	ticker := time.NewTicker(browserReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			r.tick()
		}
	}
}
