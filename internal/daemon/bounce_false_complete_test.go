package daemon

import (
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// fix-bounce-false-complete: right after a daemon restart a SessionStatus probe
// can report a still-running session dead. The client then asks GetExitInfo,
// which used to answer a successful, EMPTY ExitInfo — and an empty ExitInfo
// reads CleanExit()==true, so a coordinator (not rolled to in_review like a
// worker) was marked complete and the finished-session reaper then killed it.

func TestGetExitInfo_NoRecordedExitIsNotACleanExit(t *testing.T) {
	c := staleTestCore()

	var never ExitInfo
	testutil.NoError(t, c.GetExitInfo(&TaskIDReq{TaskID: "t"}, &never))
	testutil.True(t, never.StreamLost)
	testutil.False(t, never.CleanExit())

	// An already-handed-out entry is also "no info for this reader".
	c.record("t", nil)
	var first, second ExitInfo
	testutil.NoError(t, c.GetExitInfo(&TaskIDReq{TaskID: "t"}, &first))
	testutil.False(t, first.StreamLost)
	testutil.NoError(t, c.GetExitInfo(&TaskIDReq{TaskID: "t"}, &second))
	testutil.True(t, second.StreamLost)
}

func TestBounceProbeDead_CoordinatorStaysInProgressAndIsNotReaped(t *testing.T) {
	d, _ := testDaemon(t)
	seedDaemonProject(t, d, t.TempDir())
	task := &model.Task{Name: "coord", Status: model.StatusInProgress}
	testutil.NoError(t, d.db.Add(task))
	bindCoordinator(t, d, task.ID)

	// The restart → probe-says-dead sequence: the daemon has no recorded exit
	// for a session that never exited, and the relay feeds what GetExitInfo says.
	var info ExitInfo
	testutil.NoError(t, d.GetExitInfo(&TaskIDReq{TaskID: task.ID}, &info))
	d.handleSessionExit(task.ID, info)

	got, err := d.db.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInProgress)

	// Even if some path still lands it complete, the reaper must not kill it.
	got.SetStatus(model.StatusComplete)
	testutil.NoError(t, d.db.Update(got))
	r := &fakeReapRunner{running: []string{task.ID}, idle: []string{task.ID}}
	f := newFinishedSessionReaper(r, d.db)
	for i := 0; i < 4; i++ {
		testutil.Equal(t, len(f.tick()), 0)
	}
	testutil.Equal(t, len(r.stopped), 0)
}

func TestFinishedReaper_KeepAliveBindings(t *testing.T) {
	for _, kind := range []db.HeraRoleKind{db.HeraKindCoordinator, db.HeraKindFreelance} {
		t.Run(string(kind), func(t *testing.T) {
			d, _ := testDaemon(t)
			task := &model.Task{Name: "x", Status: model.StatusInProgress}
			testutil.NoError(t, d.db.Add(task))
			o, err := d.db.CreateHeraOrchestrator("o", "")
			testutil.NoError(t, err)
			role, err := d.db.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "r", Kind: kind, ArgusProject: "p"})
			testutil.NoError(t, err)
			b, err := d.db.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: role.ID, ArgusTaskID: task.ID, WorktreePath: "/wt"})
			testutil.NoError(t, err)

			r := &fakeReapRunner{running: []string{task.ID}, idle: []string{task.ID}}
			f := newFinishedSessionReaper(r, d.db)
			f.tick()
			task.SetStatus(model.StatusComplete)
			testutil.NoError(t, d.db.Update(task))
			for i := 0; i < 4; i++ {
				testutil.Equal(t, len(f.tick()), 0)
			}
			testutil.True(t, f.state[task.ID].guarded) // the skip was logged (once)

			// Once the binding ends, the same finished idle session is reaped.
			testutil.NoError(t, d.db.EndHeraBinding(b.ID, "test"))
			f.tick()
			testutil.DeepEqual(t, f.tick(), []string{task.ID})
		})
	}
}

func TestFinishedReaper_WorkerBindingStillReaped(t *testing.T) {
	d, _ := testDaemon(t)
	task := &model.Task{Name: "w", Status: model.StatusInProgress}
	testutil.NoError(t, d.db.Add(task))
	bindWorker(t, d, task.ID)
	r := &fakeReapRunner{running: []string{task.ID}, idle: []string{task.ID}}
	f := newFinishedSessionReaper(r, d.db)
	f.tick()
	task.SetStatus(model.StatusComplete)
	testutil.NoError(t, d.db.Update(task))
	f.tick()
	testutil.DeepEqual(t, f.tick(), []string{task.ID})
}

type errGuardTasks struct{ fakeReapTasks }

func (errGuardTasks) LiveHeraKeepAliveBinding(string) (*db.HeraKeepAliveBinding, error) {
	return nil, errors.New("db down")
}

func TestFinishedReaper_GuardLookupErrorFailsClosed(t *testing.T) {
	r := &fakeReapRunner{running: []string{"a"}, idle: []string{"a"}}
	tasks := errGuardTasks{fakeReapTasks{"a": task("a", model.StatusInProgress, false)}}
	f := newFinishedSessionReaper(r, tasks)
	f.tick()
	tasks.fakeReapTasks["a"].Status = model.StatusComplete
	for i := 0; i < 4; i++ {
		testutil.Equal(t, len(f.tick()), 0)
	}
	testutil.True(t, f.state["a"].guarded) // the failure was logged (once)
	testutil.Equal(t, len(r.stopped), 0)
}

func TestFinishedReaper_ArchivedCoordinatorStillReaped(t *testing.T) {
	d, _ := testDaemon(t)
	task := &model.Task{Name: "c", Status: model.StatusInProgress}
	testutil.NoError(t, d.db.Add(task))
	bindCoordinator(t, d, task.ID)
	r := &fakeReapRunner{running: []string{task.ID}, idle: []string{task.ID}}
	f := newFinishedSessionReaper(r, d.db)
	f.tick()
	task.SetArchived(true)
	testutil.NoError(t, d.db.Update(task))
	f.tick()
	testutil.DeepEqual(t, f.tick(), []string{task.ID})
}

type probeRunner struct {
	fakeSupClient
	err  error
	info SessionInfo
}

func (p *probeRunner) ProbeSession(string) (SessionInfo, error) { return p.info, p.err }

func TestSessionStatus_FailedSupervisorProbeIsAnErrorNotDeath(t *testing.T) {
	r := &probeRunner{err: errors.New("dial supervisor: reconnecting")}
	c := newSessionCore(r, config.DefaultConfig, make(chan struct{}))
	var resp SessionInfo
	testutil.Error(t, c.SessionStatus(&TaskIDReq{TaskID: "t"}, &resp))

	// A probe that succeeded and found nothing is a genuine "not running".
	r.err = nil
	resp = SessionInfo{}
	testutil.NoError(t, c.SessionStatus(&TaskIDReq{TaskID: "t"}, &resp))
	testutil.False(t, resp.Alive)
}

func TestExitInfo_NoEvidenceOfExit(t *testing.T) {
	testutil.True(t, ExitInfo{}.NoEvidenceOfExit())
	for name, e := range map[string]ExitInfo{
		"err":     {Err: "x"},
		"stopped": {Stopped: true},
		"lost":    {StreamLost: true},
		"pending": {PendingRestart: true},
		"output":  {LastOutput: []byte("bye")},
	} {
		t.Run(name, func(t *testing.T) { testutil.False(t, e.NoEvidenceOfExit()) })
	}
}

func TestSessionStatus_ProbeFindsLiveSessionGetMissed(t *testing.T) {
	r := &probeRunner{info: SessionInfo{Alive: true, PID: 42}}
	c := newSessionCore(r, config.DefaultConfig, make(chan struct{}))
	var resp SessionInfo
	testutil.NoError(t, c.SessionStatus(&TaskIDReq{TaskID: "t"}, &resp))
	testutil.True(t, resp.Alive)
	testutil.Equal(t, resp.PID, 42)
	testutil.Equal(t, resp.TaskID, "t")
}
