package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/testutil"
)

type signalCall struct {
	PID int32
	Sig syscall.Signal
}

// fakeBrowserReaper wires a reaper to a scripted scan and a recording signal.
type fakeBrowserReaper struct {
	*browserReaper
	mu       sync.Mutex
	orphans  []agent.OrphanBrowser
	scanErr  error
	scans    int
	on       bool
	cfgErr   error
	calls    []signalCall
	alive    map[int32]bool // signal reports sent only for alive pids
	termErr  error
	sigKills bool // SIGKILL leaves the process dead
}

func newFakeBrowserReaper() *fakeBrowserReaper {
	f := &fakeBrowserReaper{on: true, alive: map[int32]bool{}}
	f.browserReaper = newBrowserReaper(
		func() ([]agent.OrphanBrowser, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.scans++
			return f.orphans, f.scanErr
		},
		func() (bool, error) { return f.on, f.cfgErr })
	f.signal = func(b agent.OrphanBrowser, sig syscall.Signal) (bool, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, signalCall{b.PID, sig})
		if sig == syscall.SIGTERM && f.termErr != nil {
			return false, f.termErr
		}
		return f.alive[b.PID], nil
	}
	return f
}

func (f *fakeBrowserReaper) signals() []signalCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]signalCall(nil), f.calls...)
}

func (f *fakeBrowserReaper) kill(pid int32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alive[pid] = false
}

func TestBrowserReaper_Defaults(t *testing.T) {
	testutil.Equal(t, browserReapInterval, 5*time.Second)
	testutil.Equal(t, browserReapKillGrace, 5*time.Second)
}

func TestBrowserReaper_Tick(t *testing.T) {
	orphan := agent.OrphanBrowser{PID: 100, PGID: 100, Start: 4, TaskID: "t1"}

	t.Run("orphan exits on SIGTERM: no SIGKILL", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newFakeBrowserReaper()
			f.orphans = []agent.OrphanBrowser{orphan}
			f.alive[100] = true
			testutil.Equal(t, f.tick(), 1)
			f.kill(100) // Chrome shuts down cleanly
			time.Sleep(browserReapKillGrace)
			synctest.Wait()
			// The escalation's identity-checked signal reports nothing sent.
			testutil.DeepEqual(t, f.signals(), []signalCall{{100, syscall.SIGTERM}, {100, syscall.SIGKILL}})
			f.mu.Lock()
			testutil.Equal(t, len(f.pending), 0)
			f.mu.Unlock()
		})
	})

	t.Run("unresponsive orphan is SIGKILLed after the grace", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newFakeBrowserReaper()
			f.orphans = []agent.OrphanBrowser{orphan}
			f.alive[100] = true
			f.tick()
			time.Sleep(browserReapKillGrace - time.Millisecond)
			synctest.Wait()
			testutil.Equal(t, len(f.signals()), 1) // not yet
			time.Sleep(time.Millisecond)
			synctest.Wait()
			testutil.DeepEqual(t, f.signals(), []signalCall{{100, syscall.SIGTERM}, {100, syscall.SIGKILL}})
		})
	})

	t.Run("in-flight escalation is not re-signalled by the next tick", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newFakeBrowserReaper()
			f.orphans = []agent.OrphanBrowser{orphan}
			f.alive[100] = true
			testutil.Equal(t, f.tick(), 1)
			testutil.Equal(t, f.tick(), 0)
			testutil.Equal(t, len(f.signals()), 1)
			time.Sleep(browserReapKillGrace)
			synctest.Wait()
		})
	})

	t.Run("shutdown cancels a pending escalation", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newFakeBrowserReaper()
			done := make(chan struct{})
			f.cancel = done
			f.orphans = []agent.OrphanBrowser{orphan}
			f.alive[100] = true
			f.tick()
			close(done)
			f.wg.Wait()
			testutil.DeepEqual(t, f.signals(), []signalCall{{100, syscall.SIGTERM}})
		})
	})

	t.Run("vanished browser is not counted or escalated", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newFakeBrowserReaper()
			f.orphans = []agent.OrphanBrowser{orphan} // alive[100] false
			testutil.Equal(t, f.tick(), 0)
			f.wg.Wait()
			testutil.Equal(t, len(f.signals()), 1)
		})
	})

	t.Run("SIGTERM error is logged and not escalated", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newFakeBrowserReaper()
			f.orphans = []agent.OrphanBrowser{orphan}
			f.alive[100] = true
			f.termErr = syscall.EPERM
			testutil.Equal(t, f.tick(), 0)
			f.wg.Wait()
			testutil.Equal(t, len(f.signals()), 1)
		})
	})

	t.Run("scan error closes nothing", func(t *testing.T) {
		f := newFakeBrowserReaper()
		f.orphans = []agent.OrphanBrowser{orphan}
		f.alive[100] = true
		f.scanErr = errors.New("sysctl failed")
		testutil.Equal(t, f.tick(), 0)
		testutil.Equal(t, f.tick(), 0) // logged once (dedup), still nothing
		testutil.Equal(t, len(f.signals()), 0)
		f.scanErr = nil
		testutil.Equal(t, f.lastScanErr, "sysctl failed")
		f.alive[100] = false
		f.tick()
		testutil.Equal(t, f.lastScanErr, "")
	})

	t.Run("switch off still scans but closes nothing", func(t *testing.T) {
		f := newFakeBrowserReaper()
		f.orphans = []agent.OrphanBrowser{orphan}
		f.alive[100] = true
		f.on = false
		testutil.Equal(t, f.tick(), 0)
		testutil.Equal(t, f.tick(), 0)
		testutil.Equal(t, f.scans, 2) // ownership keeps being recorded
		testutil.Equal(t, len(f.signals()), 0)
		testutil.True(t, f.wasOff)
	})

	t.Run("unreadable config closes nothing", func(t *testing.T) {
		f := newFakeBrowserReaper()
		f.orphans = []agent.OrphanBrowser{orphan}
		f.alive[100] = true
		f.cfgErr = errors.New("bad toml")
		testutil.Equal(t, f.tick(), 0)
		testutil.Equal(t, f.tick(), 0)
		testutil.Equal(t, f.lastCfgErr, "bad toml")
		testutil.Equal(t, len(f.signals()), 0)
	})

	t.Run("no orphans skips the config read", func(t *testing.T) {
		f := newFakeBrowserReaper()
		f.cfgErr = errors.New("bad toml")
		testutil.Equal(t, f.tick(), 0)
		testutil.Equal(t, f.lastCfgErr, "")
	})
}

func TestBrowserReaper_SIGKILLErrorIsLogged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeBrowserReaper()
		f.orphans = []agent.OrphanBrowser{{PID: 7, Start: 1}}
		f.alive[7] = true
		f.signal = func(b agent.OrphanBrowser, sig syscall.Signal) (bool, error) {
			if sig == syscall.SIGKILL {
				return false, syscall.EPERM
			}
			return true, nil
		}
		f.tick()
		time.Sleep(browserReapKillGrace)
		synctest.Wait()
		f.mu.Lock()
		testutil.Equal(t, len(f.pending), 0)
		f.mu.Unlock()
	})
}

func TestRunBrowserReaper_ConfigLiveAndExitsOnDone(t *testing.T) {
	prev := browserReapInterval
	browserReapInterval = 5 * time.Millisecond
	t.Cleanup(func() { browserReapInterval = prev })

	d, _ := dsServeDaemon(t)
	cfg := filepath.Join(db.DataDir(), "config.toml")
	testutil.NoError(t, os.WriteFile(cfg, []byte("[browsers]\nclose_orphaned = false\n"), 0o600))
	scans := make(chan struct{}, 1000)
	d.browserScan = func() ([]agent.OrphanBrowser, error) {
		select {
		case scans <- struct{}{}:
		default:
		}
		// Pid 0 start 0 never matches a real process identity, so even the
		// real signal seam (disabled under go test anyway) sends nothing.
		return []agent.OrphanBrowser{{PID: 0, Start: -1}}, nil
	}
	exited := make(chan struct{})
	go func() { d.runBrowserReaper(); close(exited) }()
	for range 3 {
		select {
		case <-scans: // ticks run (and scan) even while the switch is off
		case <-time.After(3 * time.Second):
			t.Fatal("loop never ticked")
		}
	}
	close(d.done)
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("browser reaper loop did not exit on done")
	}
}
