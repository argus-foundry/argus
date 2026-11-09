package tui

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/bridge"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// dirtyKickSession adds the optional ComposerDirty signal a RemoteSession has.
type dirtyKickSession struct {
	*fakeKickSession
	dirty bool
}

func (d dirtyKickSession) ComposerDirty() bool { return d.dirty }

type keepPlaceRig struct {
	app  *App
	task *model.Task
	log  string
	stop func()
}

func newKeepPlaceRig(t *testing.T, id string) *keepPlaceRig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	logPath := filepath.Join(t.TempDir(), "ux.log")
	uxlog.Close()
	testutil.NoError(t, uxlog.Init(logPath))
	t.Cleanup(uxlog.Close)
	app := New(testDB(t), agent.NewRunner(nil), false)
	// These tests exercise the automatic kick path; resize-without-restart made
	// it opt-in, so turn it on to prove the on-state is unchanged.
	testutil.NoError(t, app.db.SetConfigValue("ui.auto_rerender_on_resize", "true"))
	_, stop := wireApp(t, app)
	t.Cleanup(stop)
	task := &model.Task{Name: id, SessionID: "saved", Worktree: t.TempDir()}
	testutil.NoError(t, app.db.(interface{ Add(*model.Task) error }).Add(task))
	seedTUITranscript(t, os.Getenv("HOME"), task.Worktree, task.SessionID, time.Time{})
	return &keepPlaceRig{app: app, task: task, log: logPath, stop: stop}
}

func (r *keepPlaceRig) logged() string {
	b, _ := os.ReadFile(r.log)
	return string(b)
}

// kick runs the Tasks-view decision once and waits for it to settle: either the
// session was stopped (a kick) or a keep-place deferral was logged.
func (r *keepPlaceRig) kick(t *testing.T, sess agent.SessionHandle, stopped func() bool, scrolled func() bool, deferred func()) {
	t.Helper()
	readUI(t, r.app.tapp, func() {
		r.app.agentPane.SetTaskID(r.task.ID)
		r.app.maybeKickRerenderGated(r.task, sess, 100, deferred, scrolled)
	})
	waitRerender(t, func() bool {
		return stopped() || strings.Contains(r.logged(), "[rerender] deferred task="+r.task.ID+":")
	})
}

func TestKeepPlace_KickMatrix(t *testing.T) {
	tests := []struct {
		name     string
		scrolled bool
		bridge   *bridge.State
		dirty    bool
		setOff   bool
		wantKick bool
		wantLog  string
	}{
		{"idle bottom empty kicks", false, nil, false, false, true, ""},
		{"scrolled up defers", true, nil, false, false, false, "pane scrolled up"},
		{"draft-aware bridge draft defers", false, &bridge.State{DraftAware: true, HasDraft: true}, false, false, false, "unsent composer text"},
		{"draft-aware bridge empty beats local typed flag", false, &bridge.State{DraftAware: true}, true, false, true, ""},
		{"unaware bridge falls back to typed flag", false, &bridge.State{}, true, false, false, "unsent composer text"},
		{"no bridge typed flag defers", false, nil, true, false, false, "unsent composer text"},
		{"scrolled and draft: draft reason wins", true, nil, true, false, false, "unsent composer text"},
		{"setting off ignores scroll", true, nil, false, true, true, ""},
		{"setting off ignores draft", false, &bridge.State{DraftAware: true, HasDraft: true}, true, true, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newKeepPlaceRig(t, "kp")
			if tc.setOff {
				testutil.NoError(t, r.app.db.SetConfigValue("ui.rerender_keep_place", "false"))
			}
			base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 80}
			sess := dirtyKickSession{base, tc.dirty}
			readUI(t, r.app.tapp, func() {
				if tc.bridge != nil {
					r.app.bridgeStates = map[string]bridge.State{r.task.ID: *tc.bridge}
				}
			})
			r.kick(t, sess, base.stopCalled.Load, func() bool { return tc.scrolled }, nil)
			testutil.Equal(t, base.stopCalled.Load(), tc.wantKick)
			if tc.wantLog != "" {
				testutil.True(t, strings.Contains(r.logged(), tc.wantLog))
				// The drift anchor survives a deferral so a later bind still sees it.
				readUI(t, r.app.tapp, func() {
					testutil.Equal(t, r.app.committedCols[r.task.ID], uint16(100))
					testutil.False(t, r.app.pendingRerenderRestart[r.task.ID])
				})
			}
		})
	}
}

// Without onDeferred (Tasks view) a keep-place deferral arms kickWait; a Hera
// pane's onDeferred is invoked instead and kickWait stays empty.
func TestKeepPlace_DeferralRetryPath(t *testing.T) {
	t.Run("tasks view arms kickWait", func(t *testing.T) {
		r := newKeepPlaceRig(t, "kw")
		base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 80}
		r.kick(t, base, base.stopCalled.Load, func() bool { return true }, nil)
		readUI(t, r.app.tapp, func() {
			_, waiting := r.app.kickWait[r.task.ID]
			testutil.True(t, waiting)
		})
	})
	t.Run("hera pane callback instead", func(t *testing.T) {
		r := newKeepPlaceRig(t, "kh")
		base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 80}
		called := make(chan struct{}, 1)
		r.kick(t, base, base.stopCalled.Load, func() bool { return true }, func() { called <- struct{}{} })
		select {
		case <-called:
		case <-time.After(uiTimeout):
			t.Fatal("onDeferred not invoked")
		}
		readUI(t, r.app.tapp, func() {
			_, waiting := r.app.kickWait[r.task.ID]
			testutil.False(t, waiting)
		})
	})
}

type getRunner struct {
	*agent.Runner
	sess agent.SessionHandle
}

func (g getRunner) Get(string) agent.SessionHandle { return g.sess }

// retryDeferredKick waits for the pane to be at the bottom and the composer
// clear, then for a KickDebounce dwell, before re-running the decision.
func TestKeepPlace_RetryDeferredKick(t *testing.T) {
	r := newKeepPlaceRig(t, "retry")
	// The test pane is 80 columns wide, so a session started at 120 has drifted.
	base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 120}
	dirty := dirtyKickSession{base, true}
	readUI(t, r.app.tapp, func() {
		r.app.runner = getRunner{Runner: agent.NewRunner(nil), sess: dirty}
		r.app.agentPane.SetTaskID(r.task.ID)
		r.app.kickWait[r.task.ID] = time.Time{}

		// The mod reports a draft: stays armed with no dwell started.
		r.app.bridgeStates = map[string]bridge.State{r.task.ID: {DraftAware: true, HasDraft: true}}
		r.app.retryDeferredKick(r.task.ID)
		testutil.True(t, r.app.kickWait[r.task.ID].IsZero())

		// Clear: the first clear tick only starts the dwell.
		r.app.bridgeStates = map[string]bridge.State{r.task.ID: {DraftAware: true}}
		dirty.dirty = false
		r.app.runner = getRunner{Runner: agent.NewRunner(nil), sess: dirty}
		r.app.retryDeferredKick(r.task.ID)
		testutil.False(t, r.app.kickWait[r.task.ID].IsZero())
		testutil.False(t, base.stopCalled.Load())

		// Before the dwell has elapsed nothing happens.
		r.app.retryDeferredKick(r.task.ID)
		_, waiting := r.app.kickWait[r.task.ID]
		testutil.True(t, waiting)

		// After it: the entry is consumed and the decision re-runs.
		r.app.kickWait[r.task.ID] = time.Now().Add(-2 * hera.KickDebounce)
		r.app.retryDeferredKick(r.task.ID)
		_, waiting = r.app.kickWait[r.task.ID]
		testutil.False(t, waiting)
	})
	waitRerender(t, func() bool {
		return strings.Contains(r.logged(), "retrying deferred kick task="+r.task.ID)
	})
	// The decision genuinely re-ran and, with the pane at the bottom and the
	// composer clear, kicked: a retry that only logged would leave this unset.
	waitRerender(t, base.stopCalled.Load)
}

// The Hera page's scroll hold must follow the live ui.rerender_keep_place setting;
// an unwired page would keep holding with the setting off.
func TestKeepPlace_HeraPageFollowsSetting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := New(testDB(t), agent.NewRunner(nil), false)
	testutil.True(t, app.heraPage.KeepPlaceOn())
	testutil.NoError(t, app.db.SetConfigValue("ui.rerender_keep_place", "false"))
	testutil.False(t, app.heraPage.KeepPlaceOn())
	testutil.NoError(t, app.db.SetConfigValue("ui.rerender_keep_place", "true"))
	testutil.True(t, app.heraPage.KeepPlaceOn())
}

func TestKeepPlace_RetryDropsEntryWhenSessionGone(t *testing.T) {
	r := newKeepPlaceRig(t, "gone")
	readUI(t, r.app.tapp, func() {
		r.app.kickWait[r.task.ID] = time.Time{}
		r.app.retryDeferredKick(r.task.ID) // runner has no session for the task
		_, waiting := r.app.kickWait[r.task.ID]
		testutil.False(t, waiting)
		r.app.retryDeferredKick("never-armed") // no entry: no-op
	})
}

// The real tick drives the Tasks-view retry for the task shown in the agent
// view: with the pane at the bottom and the composer clear, the armed wait
// starts its dwell without any key event.
func TestKeepPlace_TickDrivesRetryForViewedTask(t *testing.T) {
	r := newKeepPlaceRig(t, "tick")
	base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 80}
	readUI(t, r.app.tapp, func() {
		r.app.runner = getRunner{Runner: agent.NewRunner(nil), sess: base}
		r.app.mu.Lock()
		r.app.mode = modeAgent
		r.app.agentState.TaskID = r.task.ID
		r.app.mu.Unlock()
		r.app.agentPane.SetTaskID(r.task.ID)
		r.app.agentPane.SetSession(base)
		r.app.kickWait[r.task.ID] = time.Time{}
	})
	r.app.onTick()
	waitRerender(t, func() bool {
		var started bool
		readUI(t, r.app.tapp, func() { started = !r.app.kickWait[r.task.ID].IsZero() })
		return started
	})
}

// screenKickSession serves a fixed output tail so the kick path can read the
// rendered Claude composer, plus the optional typed-since-submit flag.
type screenKickSession struct {
	*fakeKickSession
	tail  []byte
	dirty bool
}

func (s screenKickSession) RecentOutputTail(int) []byte { return s.tail }
func (s screenKickSession) PTYSize() (int, int)         { return 80, 24 }
func (s screenKickSession) ComposerDirty() bool         { return s.dirty }

// composerFrame paints a Claude-style composer; the prompt marker is "❯" + NBSP.
func composerFrame(body string) []byte {
	return []byte("\x1b[?1049h\x1b[2J\x1b[7;1H❯\u00a0" + body + "\x1b[7;20H")
}

// Draft-source order for a Claude session with no draft-aware bridge: the
// rendered composer answers when it finds the composer (so a stale typed flag
// cannot hold the kick); an unknown screen falls through to the flag.
func TestKeepPlace_ScreenProbeOrder(t *testing.T) {
	tests := []struct {
		name     string
		tail     []byte
		dirty    bool
		wantKick bool
	}{
		{"screen shows typed text, flag clear: defers", composerFrame("fix the bug"), false, false},
		{"screen shows empty composer, stale flag: kicks", composerFrame(""), true, true},
		{"composer not found falls through to flag: defers", []byte("no composer here"), true, false},
		{"composer not found, no flag: kicks", nil, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newKeepPlaceRig(t, "probe")
			base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 80}
			sess := screenKickSession{base, tc.tail, tc.dirty}
			r.kick(t, sess, base.stopCalled.Load, func() bool { return false }, nil)
			testutil.Equal(t, base.stopCalled.Load(), tc.wantKick)
		})
	}
}

// A draft-aware bridge is asked instead of re-emulating the output: its empty
// report kicks even though the screen shows text.
func TestKeepPlace_BridgeOutranksScreen(t *testing.T) {
	r := newKeepPlaceRig(t, "bridgefirst")
	base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 80}
	sess := screenKickSession{base, composerFrame("fix the bug"), true}
	readUI(t, r.app.tapp, func() {
		r.app.bridgeStates = map[string]bridge.State{r.task.ID: {DraftAware: true}}
	})
	r.kick(t, sess, base.stopCalled.Load, func() bool { return false }, nil)
	testutil.True(t, base.stopCalled.Load())
}

// The typed-since-submit flag cannot see Backspace emptying the box, so it
// releases the kick after agent.KickDeferCap at one width; a younger deferral,
// or one recorded at another width, still holds.
func TestKeepPlace_TypedFlagDeferralCap(t *testing.T) {
	tests := []struct {
		name     string
		since    time.Duration
		cols     uint16
		wantKick bool
	}{
		{"just under the cap holds", agent.KickDeferCap - time.Minute, 100, false},
		{"past the cap releases", agent.KickDeferCap + time.Minute, 100, true},
		{"a deferral at another width does not count", agent.KickDeferCap + time.Minute, 90, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newKeepPlaceRig(t, "cap")
			base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 80}
			sess := dirtyKickSession{base, true}
			readUI(t, r.app.tapp, func() {
				r.app.kickDeferSince[r.task.ID] = kickDefer{cols: tc.cols, at: time.Now().Add(-tc.since)}
			})
			r.kick(t, sess, base.stopCalled.Load, func() bool { return false }, nil)
			testutil.Equal(t, base.stopCalled.Load(), tc.wantKick)
			if tc.wantKick {
				testutil.True(t, strings.Contains(r.logged(), "typed-flag deferral cap reached"))
				readUI(t, r.app.tapp, func() {
					_, held := r.app.kickDeferSince[r.task.ID]
					testutil.False(t, held) // a kick clears the deferral clock
				})
			}
		})
	}
}

// A first deferral starts the per-width clock and a draft deferral spaces the
// next full evaluation by KickRetryInterval.
func TestKeepPlace_DeferralStartsClockAndSpacesRetry(t *testing.T) {
	r := newKeepPlaceRig(t, "clock")
	base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 80}
	sess := dirtyKickSession{base, true}
	r.kick(t, sess, base.stopCalled.Load, func() bool { return false }, nil)
	readUI(t, r.app.tapp, func() {
		d, ok := r.app.kickDeferSince[r.task.ID]
		testutil.True(t, ok)
		testutil.Equal(t, d.cols, uint16(100))
		testutil.True(t, r.app.kickRetryAfter[r.task.ID].After(time.Now().Add(hera.KickRetryInterval/2)))
	})
}

// During a tick-driven retry only the cheap local signals hold the dwell; the
// typed flag does not (the full evaluation weighs it), and the retry interval
// after a draft deferral postpones that evaluation.
func TestKeepPlace_RetryUsesCheapSignalsAndSpacing(t *testing.T) {
	r := newKeepPlaceRig(t, "cheap")
	base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 120}
	sess := dirtyKickSession{base, true} // stale typed flag
	readUI(t, r.app.tapp, func() {
		r.app.runner = getRunner{Runner: agent.NewRunner(nil), sess: sess}
		r.app.agentPane.SetTaskID(r.task.ID)
		r.app.kickWait[r.task.ID] = time.Time{}
		r.app.retryDeferredKick(r.task.ID)
		testutil.False(t, r.app.kickWait[r.task.ID].IsZero()) // flag alone did not hold the dwell

		// Dwell elapsed but the post-deferral spacing has not: still waiting.
		r.app.kickWait[r.task.ID] = time.Now().Add(-2 * hera.KickDebounce)
		r.app.kickRetryAfter[r.task.ID] = time.Now().Add(time.Minute)
		r.app.retryDeferredKick(r.task.ID)
		_, waiting := r.app.kickWait[r.task.ID]
		testutil.True(t, waiting)
		testutil.False(t, strings.Contains(r.logged(), "retrying deferred kick"))
	})
}

// countingRunner counts Get calls so a test can prove a code path never asked
// the runner for a session.
type countingRunner struct {
	*agent.Runner
	gets atomic.Int64
}

func (c *countingRunner) Get(id string) agent.SessionHandle {
	c.gets.Add(1)
	return c.Runner.Get(id)
}

// The 1s tick calls retryDeferredKick for the viewed task. With no kick armed
// it must do nothing: no session lookup, no state, no log.
func TestKeepPlace_IdleTickDoesNothing(t *testing.T) {
	r := newKeepPlaceRig(t, "idle")
	cr := &countingRunner{Runner: agent.NewRunner(nil)}
	var before string
	readUI(t, r.app.tapp, func() {
		r.app.runner = cr
		before = r.logged()
		for i := 0; i < 500; i++ {
			r.app.retryDeferredKick(r.task.ID)
		}
		testutil.Equal(t, cr.gets.Load(), int64(0))
		testutil.Equal(t, len(r.app.kickWait), 0)
		testutil.Equal(t, len(r.app.kickDeferSince), 0)
		testutil.Equal(t, len(r.app.kickRetryAfter), 0)
		testutil.Equal(t, r.logged(), before)
	})
}

// Dropping a vanished session's wait also forgets its deferral clock.
func TestKeepPlace_DropKickWaitClearsAllState(t *testing.T) {
	r := newKeepPlaceRig(t, "drop")
	readUI(t, r.app.tapp, func() {
		id := r.task.ID
		r.app.kickWait[id] = time.Time{}
		r.app.kickDeferSince[id] = kickDefer{cols: 100, at: time.Now()}
		r.app.kickRetryAfter[id] = time.Now()
		r.app.retryDeferredKick(id) // runner has no session: drops everything
		testutil.Equal(t, len(r.app.kickWait), 0)
		testutil.Equal(t, len(r.app.kickDeferSince), 0)
		testutil.Equal(t, len(r.app.kickRetryAfter), 0)
	})
}
