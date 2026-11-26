package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// fakeReclaimRunner reports a live session for a fixed taskID (HasSession)
// and records whether/how Stop was called — WITHOUT spawning a real process
// — so heraReclaimAndArchiveTask's marker-arming logic can be exercised
// deterministically, mirroring slowStopRunner in heraactions_cascade_race_test.go.
type fakeReclaimRunner struct {
	*agent.Runner
	liveTaskID string
	stopErr    error
	stopped    chan struct{}
}

type connectedReclaimRunner struct {
	*agent.Runner
	session   agent.SessionHandle
	attachErr error
	stop      func(string, int) error
	checkErr  error
}

func (r connectedReclaimRunner) AttachableSession(string) (agent.SessionHandle, error) {
	return r.session, r.attachErr
}

func (r connectedReclaimRunner) HasSession(string) bool { return r.session != nil }

func (r connectedReclaimRunner) StopTreeForPID(id string, pid int) error {
	return r.stop(id, pid)
}

func (r connectedReclaimRunner) CheckReclaimSessionRetired(string) error { return r.checkErr }

func TestHeraReclaimConnectedArmsSessionBeforeStop(t *testing.T) {
	d := testDB(t)
	task := &model.Task{ID: "tw", Name: "tw", Status: model.StatusInProgress}
	const pid = 42
	type observation struct {
		armed, locked bool
		taskID        string
		pid           int
		err           error
	}
	observed := make(chan observation, 1)
	runner := connectedReclaimRunner{
		Runner:  agent.NewRunner(nil),
		session: &fakeKickSession{pid: pid, alive: true},
		stop: func(id string, expectedPID int) error {
			armed, err := d.ConsumeHeraReclaimStop(task.ID, pid)
			unlock, acquired, lockErr := d.TryLockSessionMutation(task.ID)
			if acquired {
				unlock()
			}
			observed <- observation{armed: armed, locked: !acquired, taskID: id, pid: expectedPID, err: errors.Join(err, lockErr)}
			return nil
		},
	}
	app := New(d, runner, true)
	testutil.NoError(t, d.Add(task))
	app.heraReclaimAndArchiveTask(task.ID)
	select {
	case got := <-observed:
		testutil.NoError(t, got.err)
		testutil.True(t, got.armed)
		testutil.True(t, got.locked)
		testutil.Equal(t, got.taskID, task.ID)
		testutil.Equal(t, got.pid, pid)
	case <-time.After(2 * time.Second):
		t.Fatal("reclaim did not stop the active session")
	}
	row, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, row.Archived)
	testutil.Equal(t, row.Status, model.StatusInProgress)
	testutil.False(t, app.consumeHeraReclaimPending(task.ID))
}

func TestHeraReclaimConnectedUnconfirmedPreservesWorktree(t *testing.T) {
	const pid = 42
	for _, tc := range []struct {
		name        string
		attachErr   error
		checkErr    error
		stopErr     error
		wantPending bool
	}{
		{name: "launch pending", attachErr: agent.ErrSessionStarting},
		{name: "empty slot unconfirmed", checkErr: agent.ErrSessionCoordinationUnsupported},
		{name: "unsupported stop", stopErr: agent.ErrSessionCoordinationUnsupported},
		{name: "replacement", stopErr: agent.ErrSessionNotFound},
		{name: "stop response lost", stopErr: agent.ErrSessionStopUnconfirmed, wantPending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDB(t)
			worktree := filepath.Join(t.TempDir(), ".argus", "worktrees", "worker")
			testutil.NoError(t, os.MkdirAll(worktree, 0o750))
			task := &model.Task{ID: "tw", Name: "tw", Status: model.StatusInProgress, Worktree: worktree}
			var stopCalls int
			runner := connectedReclaimRunner{
				Runner:    agent.NewRunner(nil),
				attachErr: tc.attachErr,
				checkErr:  tc.checkErr,
				stop: func(id string, expectedPID int) error {
					stopCalls++
					testutil.Equal(t, id, task.ID)
					testutil.Equal(t, expectedPID, pid)
					return tc.stopErr
				},
			}
			wantCalls := 0
			if tc.stopErr != nil {
				runner.session = &fakeKickSession{pid: pid, alive: true}
				wantCalls = 1
			}
			app := New(d, runner, true)
			testutil.NoError(t, d.Add(task))
			app.reclaimConnectedHeraTask(runner, d, task.ID)
			testutil.Equal(t, stopCalls, wantCalls)
			pending, err := d.HeraReclaimStopPending(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, pending, tc.wantPending)
			testutil.True(t, agent.DirExists(worktree))
		})
	}
}

func TestHeraReclaimConnectedRetiredRemovesWorktree(t *testing.T) {
	d := testDB(t)
	worktree := filepath.Join(t.TempDir(), ".argus", "worktrees", "worker")
	testutil.NoError(t, os.MkdirAll(worktree, 0o750))
	task := &model.Task{ID: "tw", Name: "tw", Status: model.StatusInReview, Worktree: worktree}
	testutil.NoError(t, d.Add(task))
	runner := connectedReclaimRunner{Runner: agent.NewRunner(nil)}
	app := New(d, runner, true)

	app.reclaimConnectedHeraTask(runner, d, task.ID)

	testutil.False(t, agent.DirExists(worktree))
	_, err := d.Get(task.ID)
	testutil.ErrorIs(t, err, db.ErrTaskNotFound)
}

func TestHeraReclaimConnectedTickLeavesArmedTaskInProgress(t *testing.T) {
	d := testDB(t)
	const pid = 42
	task := &model.Task{ID: "tw", Name: "tw", Status: model.StatusInProgress, AgentPID: pid, Archived: true}
	testutil.NoError(t, d.Add(task))
	armed, err := d.ArmHeraReclaimStop(task.ID, pid)
	testutil.NoError(t, err)
	testutil.True(t, armed)
	app := New(d, agent.NewRunner(nil), true)

	app.mu.Lock()
	app.refreshTasksWithIDs([]string{}, nil, false)
	app.mu.Unlock()

	row, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInProgress)
	matched, err := d.ConsumeHeraReclaimStop(task.ID, pid)
	testutil.NoError(t, err)
	testutil.True(t, matched)
}

func newFakeReclaimRunner(liveTaskID string) *fakeReclaimRunner {
	return &fakeReclaimRunner{Runner: agent.NewRunner(nil), liveTaskID: liveTaskID, stopped: make(chan struct{}, 1)}
}

func (r *fakeReclaimRunner) HasSession(taskID string) bool { return taskID == r.liveTaskID }

func (r *fakeReclaimRunner) Stop(taskID string) error {
	select {
	case r.stopped <- struct{}{}:
	default:
	}
	return r.stopErr
}

func (r *fakeReclaimRunner) waitStopped(t *testing.T) {
	t.Helper()
	select {
	case <-r.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop was never called")
	}
}

// TestHeraReclaimAndArchiveTask_LiveTaskCompletesAfterAsyncExit is the
// fix-nuke-completion-race regression: a task actively `in_progress` at the
// moment of reclaim must still reach `complete` once its reclaim-triggered
// stop's exit is observed later — not get permanently stranded at
// `in_review` by the ordinary crash/stop/fast-fail rule in
// handleSessionExitUI, which has no way to distinguish a deliberate,
// terminal nuke stop from an ordinary recoverable one.
func TestHeraReclaimAndArchiveTask_LiveTaskCompletesAfterAsyncExit(t *testing.T) {
	d := testDB(t)
	t.Setenv("HOME", t.TempDir())
	runner := newFakeReclaimRunner("tw")
	app := New(d, runner, false)

	testutil.NoError(t, d.Add(&model.Task{ID: "tw", Name: "tw", Status: model.StatusInProgress, Project: "p", CreatedAt: time.Now()}))

	reclaimed := app.heraReclaimAndArchiveTask("tw")
	testutil.Equal(t, reclaimed, false) // no worktree/branch on this task, nothing to reclaim

	runner.waitStopped(t)

	// The archive-time snapshot must NOT force completion synchronously —
	// that's the existing, correct behavior from fix-hera-archive-status.
	task, err := d.Get("tw")
	testutil.NoError(t, err)
	testutil.Equal(t, task.Archived, true)
	testutil.Equal(t, task.Status, model.StatusInProgress)

	app.mu.Lock()
	armed := app.pendingHeraReclaim["tw"]
	app.mu.Unlock()
	testutil.Equal(t, armed, true) // marker survives the successful Stop() call

	// Simulate the async exit notification arriving later — a nuke's own
	// stop is never a "clean" exit (it's a signal), so cleanExit=false here,
	// exactly like an ordinary recoverable stop/crash would report.
	app.handleSessionExitUI("tw", false /* cleanExit */, false /* pendingRestart */)

	task, err = d.Get("tw")
	testutil.NoError(t, err)
	testutil.Equal(t, task.Status, model.StatusComplete) // NOT in_review — the regression this fixes

	app.mu.Lock()
	stillArmed := app.pendingHeraReclaim["tw"]
	app.mu.Unlock()
	testutil.Equal(t, stillArmed, false) // consumed — cannot leak into a later, unrelated exit
}

// TestHeraReclaimAndArchiveTask_NoLiveSessionMarkerDoesNotLeak covers the
// cleanup path: a task snapshotted in_progress but with no live runner
// session at reclaim time (HasSession false) never gets a Stop() call, so no
// exit notification is ever coming to consume the marker — it must be
// cleared immediately rather than left to leak into a later, unrelated exit
// of the same task ID.
func TestHeraReclaimAndArchiveTask_NoLiveSessionMarkerDoesNotLeak(t *testing.T) {
	d := testDB(t)
	t.Setenv("HOME", t.TempDir())
	app := New(d, agent.NewRunner(nil), false) // no session registered for "tw"

	testutil.NoError(t, d.Add(&model.Task{ID: "tw", Name: "tw", Status: model.StatusInProgress, Project: "p", CreatedAt: time.Now()}))

	app.heraReclaimAndArchiveTask("tw")

	task, err := d.Get("tw")
	testutil.NoError(t, err)
	testutil.Equal(t, task.Archived, true)
	testutil.Equal(t, task.Status, model.StatusInProgress) // untouched, matches existing behavior

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		armed := app.pendingHeraReclaim["tw"]
		app.mu.Unlock()
		if !armed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pendingHeraReclaim marker leaked with no live session to ever consume it")
}

// TestHandleSessionExitUI_ReclaimMarker_WorkerBindingStillWins pins the PR
// #707 / BUG-050 invariant as defense-in-depth: even with the reclaim marker
// set, a task that still holds a live worker-kind hera binding at exit time
// rolls to in_review via RollHeraWorkerToReview, never force-completed. In
// the real nuke path the binding is always ended before the stop fires, so
// this is not reachable in production — but the ordering must not silently
// invert if that assumption is ever violated.
func TestHandleSessionExitUI_ReclaimMarker_WorkerBindingStillWins(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)

	orch := seedHeraOrch(t, d, "o")
	seedHeraBoundRole(t, d, orch, "w", db.HeraKindWorker, "tw") // adds task "tw" InProgress + a LIVE binding

	app.mu.Lock()
	app.pendingHeraReclaim["tw"] = true
	app.mu.Unlock()

	app.handleSessionExitUI("tw", false /* cleanExit */, false /* pendingRestart */)

	task, err := d.Get("tw")
	testutil.NoError(t, err)
	testutil.Equal(t, task.Status, model.StatusInReview) // invariant wins, not forced to complete

	app.mu.Lock()
	stillArmed := app.pendingHeraReclaim["tw"]
	app.mu.Unlock()
	testutil.Equal(t, stillArmed, false) // still consumed even though the roll branch fired
}
