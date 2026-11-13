package api

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

type stoppingSessionRunner struct {
	agent.SessionRunner
	session agent.SessionHandle
}

func (r stoppingSessionRunner) Get(string) agent.SessionHandle { return r.session }
func (r stoppingSessionRunner) Stop(string) error              { return r.session.Stop() }

func TestBulkDelete_ConfirmsRealSessionExit(t *testing.T) {
	s, d, _, _, _ := bulkSetup(t)
	task := addTask(t, d, "real-stop")
	readyR, readyW, err := os.Pipe()
	testutil.NoError(t, err)
	defer readyR.Close()
	defer readyW.Close()
	cmd := exec.Command("/bin/sh", "-c", `trap '' TERM HUP; printf x >&3; while read ignored; do :; done`)
	cmd.Dir = task.Worktree
	cmd.ExtraFiles = []*os.File{readyW}
	session, err := agent.StartSession(task.ID, cmd, 24, 80)
	testutil.NoError(t, err)
	defer func() { _ = session.Cmd.Process.Kill(); <-session.Done() }()
	var b [1]byte
	_, err = io.ReadFull(readyR, b[:])
	testutil.NoError(t, err)
	s.runner = stoppingSessionRunner{s.runner, session}
	s.deleteOps = observingDeleteOps{func(string, string, string) agent.GitCleanupResult {
		testutil.Equal(t, session.Alive(), false)
		return agent.GitCleanupResult{}
	}}
	doc := seedDeleteJob(t, d, "real-stop", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	testutil.NoError(t, s.processDeleteTask("real-stop", &doc, &doc.Results[0]))
	testutil.Equal(t, session.Alive(), false)
	testutil.Equal(t, doc.Results[0].State, deleteTaskSucceeded)
}

type unexitedSession struct {
	agent.SessionHandle
	done chan struct{}
}

func (s unexitedSession) Done() <-chan struct{} { return s.done }
func (s unexitedSession) Alive() bool           { return true }
func (s unexitedSession) PID() int              { return 1 }
func (s unexitedSession) Stop() error           { return nil }

func TestBulkDelete_UnconfirmedExitPreservesResources(t *testing.T) {
	s, d, ops, _, _ := bulkSetup(t)
	task := addTask(t, d, "unexited")
	log := agent.SessionLogPath(task.ID)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(log), 0700))
	testutil.NoError(t, os.WriteFile(log, []byte("keep"), 0600))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	s.deleteJobCtx = ctx
	s.deleteJobCancel = cancel
	s.runner = stoppingSessionRunner{s.runner, unexitedSession{done: make(chan struct{})}}
	doc := seedDeleteJob(t, d, "unexited", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	testutil.NoError(t, s.processDeleteTask("unexited", &doc, &doc.Results[0]))
	testutil.Equal(t, doc.Results[0].State, deleteTaskFailed)
	testutil.Equal(t, doc.Results[0].RecordDeleted, false)
	_, err := d.Get(task.ID)
	testutil.NoError(t, err)
	_, err = os.Stat(log)
	testutil.NoError(t, err)
	testutil.Equal(t, len(ops.cleanups), 0)
}

func TestBulkDelete_RefusesUnfinishedCreation(t *testing.T) {
	s, d, _, _, _ := bulkSetup(t)
	repo := t.TempDir()
	bulkGit(t, repo, "init", "-b", "master")
	bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
	testutil.NoError(t, d.SetProject("start", config.Project{Path: repo, Branch: "HEAD"}))
	testutil.NoError(t, d.SetBackend("test", config.Backend{Command: "sleep 60 #"}))
	runner := agent.NewRunner(nil)
	s.runner = runner
	ready, resume, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, _, err := agent.CreateAndStart(d, runner, agent.CreateInput{Project: "start", Name: "new", Backend: "test", BeforeStart: func() { close(ready); <-resume }})
		finished <- err
	}()
	<-ready
	tasks, err := d.TasksChecked()
	testutil.NoError(t, err)
	testutil.Equal(t, len(tasks), 1)
	task := tasks[0]
	doc := seedDeleteJob(t, d, "start", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	res := &doc.Results[0]
	testutil.NoError(t, s.processDeleteTask("start", &doc, res))
	close(resume)
	testutil.NoError(t, <-finished)
	if session := runner.Get(task.ID); session != nil {
		defer func() { _ = session.Stop(); <-session.Done() }()
	}
	testutil.Equal(t, res.State, deleteTaskSkipped)
	if res.Error == nil {
		t.Fatal("missing task_creation_in_progress code")
	}
	testutil.Equal(t, *res.Error, "task_creation_in_progress")
	_, err = d.Get(task.ID)
	testutil.NoError(t, err)
	_, err = os.Stat(task.Worktree)
	testutil.NoError(t, err)
}

type verifyingExitRunner struct {
	agent.SessionRunner
	verify func(context.Context, string, int) error
}

func (r verifyingExitRunner) WaitForSessionExit(ctx context.Context, id string, pid int) error {
	return r.verify(ctx, id, pid)
}

func TestBulkDelete_RemoteExitUncertaintyPreservesRecord(t *testing.T) {
	s, d, ops, _, _ := bulkSetup(t)
	task := addTask(t, d, "remote-stop")
	confirmed := false
	s.runner = verifyingExitRunner{s.runner, func(context.Context, string, int) error {
		confirmed = true
		return fmt.Errorf("supervisor disconnected")
	}}
	doc := seedDeleteJob(t, d, "remote-stop", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	testutil.NoError(t, s.processDeleteTask("remote-stop", &doc, &doc.Results[0]))
	testutil.True(t, confirmed)
	testutil.Equal(t, doc.Results[0].State, deleteTaskFailed)
	_, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(ops.cleanups), 0)
}

type pendingExitRunner struct{ agent.SessionRunner }

func (pendingExitRunner) HasPendingRestart(string) bool { return true }

func TestBulkDelete_PendingRestartPreservesRecord(t *testing.T) {
	s, d, ops, stop, _ := bulkSetup(t)
	task := addTask(t, d, "restarting")
	s.runner = pendingExitRunner{s.runner}
	doc := seedDeleteJob(t, d, "restarting", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	testutil.NoError(t, s.processDeleteTask("restarting", &doc, &doc.Results[0]))
	testutil.Equal(t, doc.Results[0].State, deleteTaskFailed)
	_, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(stop.stopped), 0)
	testutil.Equal(t, len(ops.cleanups), 0)
}
