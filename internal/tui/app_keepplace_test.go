package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/bridge"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
	"github.com/drn/argus/internal/tui/hera"
	"github.com/drn/argus/internal/uxlog"
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
				testutil.NoError(t, r.app.db.SetConfigValue("bridge.keep_place", "false"))
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
	base := &fakeKickSession{pid: 101, alive: true, idle: true, initCols: 80}
	dirty := dirtyKickSession{base, true}
	readUI(t, r.app.tapp, func() {
		r.app.runner = getRunner{Runner: agent.NewRunner(nil), sess: dirty}
		r.app.agentPane.SetTaskID(r.task.ID)
		r.app.kickWait[r.task.ID] = time.Time{}

		// Still has a draft: stays armed with no dwell started.
		r.app.retryDeferredKick(r.task.ID)
		testutil.True(t, r.app.kickWait[r.task.ID].IsZero())

		// Clear: the first clear tick only starts the dwell.
		dirty.dirty = false
		r.app.runner = getRunner{Runner: agent.NewRunner(nil), sess: dirty}
		r.app.retryDeferredKick(r.task.ID)
		testutil.False(t, r.app.kickWait[r.task.ID].IsZero())
		testutil.False(t, base.stopCalled.Load())

		// Before the dwell has elapsed nothing happens.
		r.app.retryDeferredKick(r.task.ID)
		_, waiting := r.app.kickWait[r.task.ID]
		testutil.True(t, waiting)

		// After it: the entry is consumed and the decision re-runs (committed
		// cols are seeded so the kick has drift to act on).
		r.app.kickWait[r.task.ID] = time.Now().Add(-2 * hera.KickDebounce)
		r.app.committedCols[r.task.ID] = 0
		r.app.retryDeferredKick(r.task.ID)
		_, waiting = r.app.kickWait[r.task.ID]
		testutil.False(t, waiting)
	})
	waitRerender(t, func() bool {
		return strings.Contains(r.logged(), "retrying deferred kick task="+r.task.ID)
	})
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
