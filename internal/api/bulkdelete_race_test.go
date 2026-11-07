package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBulkDelete_SerializesHTTPStartup(t *testing.T) {
	for _, action := range []string{"resume", "restart"} {
		t.Run(action, func(t *testing.T) {
			s, d, _, _, _ := bulkSetup(t)
			repo := t.TempDir()
			bulkGit(t, repo, "init", "-b", "master")
			bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
			testutil.NoError(t, d.SetProject("race", config.Project{Path: repo}))
			testutil.NoError(t, d.SetBackend("test", config.Backend{Command: "sleep 60 #"}))
			wt := agent.WorktreeDir("race", "existing")
			bulkGit(t, repo, "worktree", "add", "-b", "argus/existing", wt)
			task := &model.Task{Name: "existing", Project: "race", Backend: "test", Worktree: wt, Branch: "argus/existing", Status: model.StatusInReview}
			testutil.NoError(t, d.Add(task))
			runner := agent.NewRunner(nil)
			s.runner = runner
			doc := seedDeleteJob(t, d, "http-race", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
			synctest.Test(t, func(t *testing.T) {
				ready, start, allowCleanup, resumed, deleted := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				restore := agent.SetPrelaunchForTest(func(context.Context, *model.Task, config.Config) error { close(ready); <-start; return nil })
				defer restore()
				cleanupEntered := make(chan struct{})
				s.deleteOps = observingDeleteOps{func(w, b, r string) agent.GitCleanupResult {
					close(cleanupEntered)
					<-allowCleanup
					if session := runner.Get(task.ID); session != nil {
						testutil.Equal(t, session.Alive(), false)
					}
					return agent.CleanupTaskGitContext(context.Background(), w, b, r)
				}}
				response := httptest.NewRecorder()
				go func() {
					defer close(resumed)
					s.routes().ServeHTTP(response, authedReq("POST", "/api/tasks/"+task.ID+"/"+action, ""))
				}()
				<-ready
				go func() {
					defer close(deleted)
					testutil.NoError(t, s.processDeleteTask("http-race", &doc, &doc.Results[0]))
				}()
				synctest.Wait()
				select {
				case <-cleanupEntered:
					t.Error("delete passed reserved HTTP startup")
				default:
				}
				_, err := d.Get(task.ID)
				testutil.NoError(t, err)
				close(start)
				<-resumed
				close(allowCleanup)
				<-deleted
				testutil.Equal(t, response.Code, http.StatusOK)
				var body map[string]any
				testutil.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				pid, published := body["pid"].(float64)
				testutil.True(t, published && pid > 0)
				testutil.Equal(t, doc.Results[0].State, deleteTaskSucceeded)
				if session := runner.Get(task.ID); session != nil {
					testutil.Equal(t, session.Alive(), false)
					_ = session.Stop()
					<-session.Done()
				}
				_, err = d.Get(task.ID)
				testutil.ErrorIs(t, err, db.ErrTaskNotFound)
				_, err = os.Stat(wt)
				testutil.True(t, os.IsNotExist(err))
				s.deleteJobCancel()
				time.Sleep(5 * time.Second) // Drain Stop's escalation timer in virtual time.
			})
		})
	}
}

func TestBulkDelete_SessionAppearedBeforeCleanup(t *testing.T) {
	for _, kind := range []string{"live", "reserved"} {
		t.Run(kind, func(t *testing.T) {
			s, d, ops, _, _ := bulkSetup(t)
			task := addTask(t, d, "appeared")
			runner := &appearingSessionRunner{SessionRunner: s.runner, reserved: true}
			if kind == "live" {
				runner.session = unexitedSession{done: make(chan struct{})}
			}
			s.runner = runner
			testutil.NoError(t, d.Delete(task.ID))
			res := storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, RecordDeleted: true}, Snap: &deleteSnap{Worktree: task.Worktree, Branch: task.Branch}}
			s.cleanupDeletedTask(&res, ops)
			testutil.Equal(t, res.State, deleteTaskFailed)
			if res.Error == nil {
				t.Fatal("missing session_appeared code")
			}
			testutil.Equal(t, *res.Error, "session_appeared")
			testutil.Equal(t, len(ops.cleanups), 0)
			_, err := os.Stat(task.Worktree)
			testutil.NoError(t, err)
		})
	}
}

type appearingSessionRunner struct {
	agent.SessionRunner
	reserved bool
	session  agent.SessionHandle
}

func (r *appearingSessionRunner) Get(string) agent.SessionHandle { return r.session }
func (r *appearingSessionRunner) HasSession(string) bool         { return r.reserved }

type checkedPresenceRunner struct {
	agent.SessionRunner
	presence func(context.Context, string) (bool, error)
}

func (r checkedPresenceRunner) SessionPresent(ctx context.Context, id string) (bool, error) {
	return r.presence(ctx, id)
}

func TestBulkDelete_SupervisorPresenceBeforeCleanup(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			s, d, ops, _, _ := bulkSetup(t)
			task := addTask(t, d, "presence")
			s.runner = checkedPresenceRunner{s.runner, func(context.Context, string) (bool, error) {
				if unavailable {
					return false, fmt.Errorf("supervisor unavailable")
				}
				return true, nil
			}}
			testutil.NoError(t, d.Delete(task.ID))
			res := storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, RecordDeleted: true}, Snap: &deleteSnap{Worktree: task.Worktree, Branch: task.Branch}}
			s.cleanupDeletedTask(&res, ops)
			testutil.Equal(t, res.State, deleteTaskFailed)
			testutil.Equal(t, len(ops.cleanups), 0)
			_, err := os.Stat(task.Worktree)
			testutil.NoError(t, err)
		})
	}
}

func TestBulkDelete_LateRealSessionPreservesGit(t *testing.T) {
	s, d, _, _, _ := bulkSetup(t)
	repo, origin := t.TempDir(), filepath.Join(t.TempDir(), "origin.git")
	bulkGit(t, repo, "init", "-b", "master")
	bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
	bulkGit(t, repo, "init", "--bare", origin)
	bulkGit(t, repo, "remote", "add", "origin", origin)
	wt, branch := agent.WorktreeDir("late", "late"), "argus/late"
	bulkGit(t, repo, "worktree", "add", "-b", branch, wt)
	bulkGit(t, repo, "push", "origin", branch)
	task := &model.Task{Name: "late", Worktree: wt, Branch: branch, Status: model.StatusInReview}
	testutil.NoError(t, d.Add(task))
	testutil.NoError(t, d.Delete(task.ID))
	cmd := exec.Command("sleep", "60")
	cmd.Dir = wt
	session, err := agent.StartSession(task.ID, cmd, 24, 80)
	testutil.NoError(t, err)
	defer func() { _ = session.Cmd.Process.Kill(); <-session.Done() }()
	s.runner = stoppingSessionRunner{s.runner, session}
	res := storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, RecordDeleted: true}, Snap: &deleteSnap{Worktree: wt, Branch: branch, RepoDir: repo}}
	s.cleanupDeletedTask(&res, gitDeleteOps{ctx: s.deleteContext()})
	testutil.Equal(t, res.State, deleteTaskFailed)
	if res.Error == nil {
		t.Fatal("missing session_appeared code")
	}
	testutil.Equal(t, *res.Error, "session_appeared")
	testutil.Equal(t, session.Alive(), true)
	_, err = os.Stat(wt)
	testutil.NoError(t, err)
	bulkGit(t, repo, "rev-parse", "--verify", "refs/heads/"+branch)
	bulkGit(t, repo, "--git-dir", origin, "rev-parse", "--verify", "refs/heads/"+branch)
}
