package api

import (
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/testutil"
)

type replacementDeleteRunner struct {
	agent.SessionRunner
	session agent.SessionHandle
}

func (r *replacementDeleteRunner) Get(string) agent.SessionHandle { return r.session }
func (r *replacementDeleteRunner) Stop(string) error {
	r.session = unexitedSession{done: make(chan struct{})}
	return nil
}

func TestBulkDelete_ReplacementDuringStopPreservesRecord(t *testing.T) {
	s, d, ops, _, _ := bulkSetup(t)
	task := addTask(t, d, "replacement")
	s.runner = &replacementDeleteRunner{SessionRunner: s.runner}
	doc := seedDeleteJob(t, d, "replacement", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	testutil.NoError(t, s.processDeleteTask("replacement", &doc, &doc.Results[0]))
	testutil.Equal(t, doc.Results[0].State, deleteTaskFailed)
	testutil.Equal(t, doc.Results[0].RecordDeleted, false)
	_, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(ops.cleanups), 0)
}

func TestBulkDelete_ExitWaitBoundPreservesResources(t *testing.T) {
	s, d, ops, _, _ := bulkSetup(t)
	task := addTask(t, d, "bound")
	log := agent.SessionLogPath(task.ID)
	artifact := filepath.Join(agent.ArtifactsDir(task.ID), "keep")
	for _, path := range []string{log, artifact} {
		testutil.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		testutil.NoError(t, os.WriteFile(path, []byte("keep"), 0600))
	}
	doc := seedDeleteJob(t, d, "bound", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	synctest.Test(t, func(t *testing.T) {
		s.runner = stoppingSessionRunner{s.runner, unexitedSession{done: make(chan struct{})}}
		start := time.Now()
		testutil.NoError(t, s.processDeleteTask("bound", &doc, &doc.Results[0]))
		testutil.True(t, time.Since(start) <= 10*time.Second)
		testutil.Equal(t, doc.Results[0].State, deleteTaskFailed)
		testutil.Equal(t, doc.Results[0].RecordDeleted, false)
		_, err := d.Get(task.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, len(ops.cleanups), 0)
		for _, path := range []string{log, artifact, task.Worktree} {
			_, err := os.Stat(path)
			testutil.NoError(t, err)
		}
		s.deleteJobCancel()
	})
}
