package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type runtimeSnapshotRunner struct {
	agent.SessionRunner
	ids []string
}

func (r *runtimeSnapshotRunner) RunningAndIdle() ([]string, []string) {
	return r.ids, nil
}

func TestEmitRuntimeChanges_PrunesDeletedTasks(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, srv.Shutdown(context.Background()))
	runner := &runtimeSnapshotRunner{SessionRunner: srv.runner, ids: []string{"deleted", "kept"}}
	srv.runner = runner
	sink := installAPISink(t)
	for _, id := range runner.ids {
		testutil.NoError(t, d.Add(&model.Task{ID: id, Name: id}))
	}
	state := newIdleWatcherState()
	srv.emitRuntimeChanges(state)
	testutil.Equal(t, len(state.runtimeNow), 2)
	testutil.NoError(t, d.Delete("deleted"))
	runner.ids = []string{"kept"}
	for range 2 {
		srv.emitRuntimeChanges(state)
		testutil.DeepEqual(t, state.runtimeNow, map[string]runtimeSig{"kept": {Live: true}})
	}
	testutil.Equal(t, len(runtimeEvents(t, sink, "kept")), 1)
	testutil.Equal(t, len(runtimeEvents(t, sink, "deleted")), 1)
	testutil.NoError(t, d.Delete("kept"))
	runner.ids = nil
	srv.emitRuntimeChanges(state)
	testutil.Equal(t, len(state.runtimeNow), 0)
}

func TestTaskRuntime_HelperIsolatesSessionHome(t *testing.T) {
	outerHome := t.TempDir()
	t.Setenv("HOME", outerHome)
	srv, d := testServer(t)
	task := &model.Task{ID: "home-isolated", Name: "n"}
	startLiveSleep(t, srv, d, task)
	for _, ext := range []string{".log", ".size"} {
		_, err := os.Stat(filepath.Join(outerHome, ".argus", "sessions", task.ID+ext))
		testutil.Equal(t, os.IsNotExist(err), true)
	}
}

func TestIdleWatcherTick_PublishesRuntime(t *testing.T) {
	srv, d := testServer(t)
	sink := installAPISink(t)
	task := &model.Task{ID: "watch-runtime", Name: "n", Status: model.StatusInReview}
	startLiveSleep(t, srv, d, task)
	state := newIdleWatcherState()
	srv.idleWatcherTick(state)
	testutil.DeepEqual(t, runtimeEvents(t, sink, task.ID), []runtimeSig{{Live: true}})
	srv.runner.(*agent.Runner).SetPendingRestartForTest(task.ID, true)
	t.Cleanup(func() { srv.runner.(*agent.Runner).SetPendingRestartForTest(task.ID, false) })
	srv.idleWatcherTick(state)
	testutil.DeepEqual(t, runtimeEvents(t, sink, task.ID), []runtimeSig{{Live: true}, {}})
}
