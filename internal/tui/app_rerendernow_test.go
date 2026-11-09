package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/bridge"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// nowRig drives the on-demand re-render with ui.auto_rerender_on_resize OFF
// (the default): the hotkey must work independent of the automatic path.
type nowRig struct {
	app  *App
	task *model.Task
	log  string
	sess *fakeKickSession
}

func newNowRig(t *testing.T, id string, initCols int) *nowRig {
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
	sess := &fakeKickSession{pid: 31, alive: true, idle: true, initCols: initCols}
	app.runner = getRunner{Runner: agent.NewRunner(nil), sess: sess}
	return &nowRig{app: app, task: task, log: logPath, sess: sess}
}

func (r *nowRig) logged() string { b, _ := os.ReadFile(r.log); return string(b) }

func (r *nowRig) press(t *testing.T, cols uint16, scrolled func() bool, onDeferred func(), want string) {
	t.Helper()
	readUI(t, r.app.tapp, func() { r.app.rerenderNow(r.task.ID, cols, scrolled, onDeferred) })
	waitRerender(t, func() bool { return strings.Contains(r.logged(), "outcome="+want) })
}

func TestRerenderNow_IdleDrift_Kicks_SettingOff(t *testing.T) {
	r := newNowRig(t, "hk-kick", 20)
	testutil.False(t, r.app.db.Config().UI.AutoRerenderOnResize)
	r.press(t, 100, nil, nil, "kicked")
	waitRerender(t, r.sess.stopCalled.Load)
	testutil.Equal(t, agent.StopScope(r.sess.stopScope.Load()), agent.StopRerender)
	readUI(t, r.app.tapp, func() { testutil.True(t, r.app.pendingRerenderRestart[r.task.ID]) })
}

func TestRerenderNow_NoDrift_IsNoop(t *testing.T) {
	r := newNowRig(t, "hk-noop", 100)
	r.press(t, 105, nil, nil, "noop-no-drift") // 5 cols < margin
	testutil.False(t, r.sess.stopCalled.Load())
	readUI(t, r.app.tapp, func() { testutil.False(t, r.app.pendingRerenderRestart[r.task.ID]) })
}

// The hotkey bypasses the attach cache (a second press at the same width is not
// swallowed as "unchanged"), but never the safety gates.
func TestRerenderNow_PromptBlocked_Defers(t *testing.T) {
	r := newNowRig(t, "hk-prompt", 20)
	logPath := agent.SessionLogPath(r.task.ID)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0o755))
	testutil.NoError(t, os.WriteFile(logPath, []byte("Do you want to proceed?\n❯ 1. Yes\n  2. No\n"), 0o644))
	r.press(t, 100, nil, nil, "deferred-prompt")
	testutil.False(t, r.sess.stopCalled.Load())
	readUI(t, r.app.tapp, func() {
		_, forced := r.app.kickForced[r.task.ID]
		testutil.True(t, forced) // Tasks view arms the retry
	})
}

func TestRerenderNow_Busy_Defers(t *testing.T) {
	r := newNowRig(t, "hk-busy", 20)
	r.sess.idle = false
	r.press(t, 100, nil, nil, "deferred-busy")
	testutil.False(t, r.sess.stopCalled.Load())
}

func TestRerenderNow_ScrolledUp_Defers(t *testing.T) {
	r := newNowRig(t, "hk-scroll", 20)
	r.press(t, 100, func() bool { return true }, nil, "deferred-scrolled")
	testutil.False(t, r.sess.stopCalled.Load())
}

func TestRerenderNow_Draft_Defers(t *testing.T) {
	r := newNowRig(t, "hk-draft", 20)
	readUI(t, r.app.tapp, func() {
		r.app.bridgeStates = map[string]bridge.State{r.task.ID: {DraftAware: true, HasDraft: true}}
	})
	r.press(t, 100, nil, nil, "deferred-draft")
	testutil.False(t, r.sess.stopCalled.Load())
}

// A Hera-pane hotkey passes a no-op onDeferred: it must not arm the Tasks-view
// kickWait retry (that would later re-fire when the task opens in the Tasks tab).
func TestRerenderNow_HeraCaller_DoesNotArmTasksRetry(t *testing.T) {
	r := newNowRig(t, "hk-hera", 20)
	r.sess.idle = false
	r.press(t, 100, nil, func() {}, "deferred-busy")
	readUI(t, r.app.tapp, func() {
		_, forced := r.app.kickForced[r.task.ID]
		_, waiting := r.app.kickWait[r.task.ID]
		testutil.False(t, forced)
		testutil.False(t, waiting)
	})
}

// A deferred Tasks-view hotkey re-render is retried forced (automatic kick off)
// once the blocker clears, and gives up after forcedRetryCap.
func TestRerenderNow_DeferredRetryRunsForced(t *testing.T) {
	r := newNowRig(t, "hk-retry", 20)
	r.sess.idle = false
	r.press(t, 100, nil, nil, "deferred-busy")
	r.sess.idle = true
	readUI(t, r.app.tapp, func() {
		r.app.agentPane.SetTaskID(r.task.ID)
		r.app.kickWait[r.task.ID] = time.Now().Add(-2 * time.Second)
		r.app.kickRetryAfter[r.task.ID] = time.Time{}
		r.app.retryDeferredKick(r.task.ID)
	})
	waitRerender(t, func() bool { return strings.Contains(r.logged(), "retrying deferred hotkey re-render") })
	// Past the cap: dropped, never retried.
	readUI(t, r.app.tapp, func() {
		r.app.kickForced[r.task.ID] = time.Now().Add(-2 * forcedRetryCap)
		r.app.kickWait[r.task.ID] = time.Now().Add(-2 * time.Second)
		r.app.retryDeferredKick(r.task.ID)
		_, still := r.app.kickForced[r.task.ID]
		testutil.False(t, still)
	})
	testutil.Contains(t, r.logged(), "hotkey retry gave up")
}

func TestRerenderNow_NoSession(t *testing.T) {
	r := newNowRig(t, "hk-nosess", 20)
	r.sess.alive = false
	readUI(t, r.app.tapp, func() { r.app.rerenderNow(r.task.ID, 100, nil, nil) })
	testutil.Contains(t, r.logged(), "outcome=no-session")
}
