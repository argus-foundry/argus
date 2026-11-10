package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func residentTeamWorker(t *testing.T, database *db.DB, resident *model.Task) (*model.Task, *db.HeraRole) {
	t.Helper()
	orch, err := database.EnsureResidentOrchestrator(resident.ID)
	testutil.NoError(t, err)
	w := &model.Task{ID: "team-worker", Name: "worker", Project: "project-a", Status: model.StatusInProgress, Worktree: t.TempDir(), Backend: "test"}
	testutil.NoError(t, database.Add(w))
	role, _, err := database.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "helper", Kind: db.HeraKindWorker, ArgusProject: w.Project}, w.ID, w.Worktree)
	testutil.NoError(t, err)
	return w, role
}

func TestResidentTeamSurvivesLifecycle(t *testing.T) {
	for _, action := range []string{"disable", "prerequisite", "crash", "bounce"} {
		t.Run(action, func(t *testing.T) {
			e := newResEnv(t)
			e.m.tick()
			resident, err := e.db.ResidentTask()
			testutil.NoError(t, err)
			worker, role := residentTeamWorker(t, e.db, resident)
			handle := &liveHandle{alive: true}
			e.run.sess[worker.ID] = handle
			binding, err := e.db.HeraLiveBindingByRole(role.ID)
			testutil.NoError(t, err)
			switch action {
			case "disable":
				e.cfg.Resident.Enabled = false
			case "prerequisite":
				e.cfg.KB.Enabled = false
			case "crash":
				delete(e.run.sess, resident.ID)
				e.now = e.now.Add(residentBackoffBase)
			case "bounce":
				e.m = newResidentManager(e.db, e.run, func() config.Config { return e.cfg })
			}
			e.m.tick()
			e.m.tick()
			switch action {
			case "disable", "prerequisite":
				testutil.Nil(t, e.run.Get(resident.ID))
			case "crash":
				testutil.Equal(t, len(e.run.starts), 2)
			case "bounce":
				testutil.Equal(t, len(e.run.starts), 1) // supervisor-owned session survived
			}
			testutil.True(t, e.run.Get(worker.ID) == handle)
			got, err := e.db.HeraLiveBindingByRole(role.ID)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, got, binding)
			testutil.Equal(t, e.m.State().RunningWorkers, 1)
			for _, id := range e.run.stopped {
				testutil.False(t, id == worker.ID)
			}
		})
	}
}

func TestResidentRunningWorkersUsesLiveMembership(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	resident, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	w, role := residentTeamWorker(t, e.db, resident)
	e.cfg.Resident.Enabled = false
	testutil.Equal(t, e.m.State().RunningWorkers, 0) // a binding alone isn't running
	e.run.sess[w.ID] = &liveHandle{alive: true}
	testutil.NoError(t, e.db.SetArchived(w.ID, true)) // clears task_meta, not membership
	testutil.NoError(t, e.db.ArchiveHeraRole(role.ID))
	testutil.Equal(t, e.m.State().RunningWorkers, 1)
	binding, err := e.db.HeraLiveBindingByRole(role.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, e.db.EndHeraBinding(binding.ID, "graduated"))
	testutil.Equal(t, e.m.State().RunningWorkers, 0)
}

// Hold the task_recycle poller at its scheduling boundary, then fire the
// actual forced-Hera path. A second constructor/map would restart twice.
func TestResidentRecycleTriggersExactlyOneRestart(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	resident, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	resident.Backend = "test"
	testutil.NoError(t, e.db.Update(resident))
	worker, role := residentTeamWorker(t, e.db, resident)
	testutil.NoError(t, e.db.UpsertHeraRoleStatus(role.ID, db.HeraStatusBlocked))
	runner := &residentRecycleCaptureRunner{}
	cfg := recycleTestConfig()
	d := New(e.db)
	d.runner = runner
	d.cfgFn = func() config.Config { return cfg }
	rc := d.taskRecycleRunner()
	var pending func()
	rc.spawn = func(f func()) { pending = f }
	testutil.NoError(t, e.db.SetMeta(resident.ID, db.HeraMetaNamespace, db.HeraMetaKeyPendingRecycle, "true"))
	testutil.NoError(t, rc.Recycle(resident.ID, "current handoff"))
	bindings, err := e.db.ListHeraLiveBindingsByTask(resident.ID)
	testutil.NoError(t, err)
	_, err = e.db.SendHeraMessage(role.ID, bindings[0].RoleID, "Need your decision", "waiting for a decision", nil)
	testutil.NoError(t, err)
	var resp StatusResp
	testutil.NoError(t, rpcFor(d).ForceRecycleCoordinator(&TaskIDReq{TaskID: resident.ID}, &resp))
	testutil.False(t, resp.OK)
	testutil.Contains(t, resp.Error, "already in progress")
	testutil.Nil(t, runner.started)
	pending()
	if runner.started == nil {
		t.Fatal("resident was not restarted")
	}
	testutil.Equal(t, runner.startCalls, 1)
	testutil.Contains(t, runner.started.Prompt, "current handoff")
	testutil.Contains(t, runner.started.Prompt, "helper")
	testutil.Contains(t, runner.started.Prompt, "project-a")
	testutil.Contains(t, runner.started.Prompt, "blocked=true")
	testutil.Contains(t, runner.started.Prompt, "waiting for a decision")
	testutil.False(t, strings.Contains(runner.started.Prompt, "Current plan-DAG"))
	got, err := e.db.HeraLiveBindingByRole(role.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.ArgusTaskID, worker.ID)
	inbox, err := e.db.HeraInbox(bindings[0].RoleID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(inbox), 1)
	meta, err := e.db.ListMeta(resident.ID, db.HeraMetaNamespace)
	testutil.NoError(t, err)
	for _, entry := range meta {
		if entry.Key == db.HeraMetaKeyPendingRecycle {
			testutil.Equal(t, entry.Value, "false")
		}
	}
}

type blockingResidentStart struct {
	residentRecycleCaptureRunner
	entered, release chan struct{}
}

func (r *blockingResidentStart) Start(task *model.Task, cfg config.Config, rows, cols uint16, resume bool) (agent.SessionHandle, error) {
	close(r.entered)
	<-r.release
	return r.residentRecycleCaptureRunner.Start(task, cfg, rows, cols, resume)
}

func TestResidentForcedRecycleOwnsSharedGuard(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	resident, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	resident.Backend = "test"
	testutil.NoError(t, e.db.Update(resident))
	_, role := residentTeamWorker(t, e.db, resident)
	bindings, err := e.db.ListHeraLiveBindingsByTask(resident.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, e.db.SetMeta(resident.ID, db.HeraMetaNamespace, db.HeraMetaKeyHandoffNote, "Hera handoff"))
	runner := &blockingResidentStart{entered: make(chan struct{}), release: make(chan struct{})}
	cfg := recycleTestConfig()
	d := New(e.db)
	d.runner = runner
	d.cfgFn = func() config.Config { return cfg }
	rc := d.taskRecycleRunner()
	done := make(chan error, 1)
	go func() {
		done <- hera.RecycleCoord(e.db, d.heraRecycleRunner(), bindings[0].RoleID, resident.SessionID, hera.RecycleHumanForced)
	}()
	select {
	case <-runner.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("forced recycle never entered restart")
	}
	err = rc.Recycle(resident.ID, "second trigger")
	close(runner.release)
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "already in progress")
	testutil.NoError(t, <-done)
	testutil.Equal(t, runner.startCalls, 1)
	testutil.Contains(t, runner.started.Prompt, "Hera handoff")
	testutil.Contains(t, runner.started.Prompt, "Standing team")
	testutil.False(t, strings.Contains(runner.started.Prompt, "second trigger"))
	_, err = e.db.HeraLiveBindingByRole(role.ID)
	testutil.NoError(t, err)
}

func TestResidentRecycleKeepsWorkerSession(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	resident, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	resident.Backend = "test"
	testutil.NoError(t, e.db.Update(resident))
	worker, role := residentTeamWorker(t, e.db, resident)
	runner := agent.NewRunner(nil)
	t.Cleanup(runner.StopAll)
	cfg := recycleTestConfig()
	bin := filepath.Join(t.TempDir(), "claude")
	testutil.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = agents ]; then echo '[]'; exit 0; fi\nexec sleep 60\n"), 0o755))
	cfg.Backends["resident-test"] = config.Backend{Command: bin}
	resident.Backend = "resident-test"
	testutil.NoError(t, e.db.Update(resident))
	workerSession, err := runner.Start(worker, cfg, 24, 80, false)
	testutil.NoError(t, err)
	_, err = runner.Start(resident, cfg, 24, 80, false)
	testutil.NoError(t, err)
	rc := newTaskRecycleRunner(e.db, runner, func() config.Config { return cfg })
	testutil.NoError(t, rc.restartResident(resident.ID, "continue coordinating"))
	testutil.True(t, runner.Get(worker.ID) == workerSession)
	testutil.True(t, workerSession.Alive())
	binding, err := e.db.HeraLiveBindingByRole(role.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, binding.ArgusTaskID, worker.ID)
}
