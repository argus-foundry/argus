package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type ambiguousDeleteStart struct{ agent.SessionProvider }

func (ambiguousDeleteStart) Start(*model.Task, config.Config, uint16, uint16, bool) (agent.SessionHandle, error) {
	return nil, agent.ErrStartAmbiguous
}

func TestBulkDelete_SettledPendingIsDeletable(t *testing.T) {
	for _, kind := range []string{"failed restart", "ambiguous creation"} {
		t.Run(kind, func(t *testing.T) {
			s, d, _, _, _ := bulkSetup(t)
			repo := t.TempDir()
			bulkGit(t, repo, "init", "-b", "master")
			bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
			testutil.NoError(t, d.SetProject("settled", config.Project{Path: repo, Branch: "HEAD"}))
			testutil.NoError(t, d.SetBackend("test", config.Backend{Command: "echo"}))
			var task *model.Task
			if kind == "ambiguous creation" {
				_, _, err := agent.CreateAndStart(d, ambiguousDeleteStart{}, agent.CreateInput{Project: "settled", Name: "settled", Backend: "test"})
				testutil.ErrorIs(t, err, agent.ErrStartAmbiguous)
				tasks, err := d.TasksChecked()
				testutil.NoError(t, err)
				testutil.Equal(t, len(tasks), 1)
				task = tasks[0]
			} else {
				wt := agent.WorktreeDir("settled", "settled")
				bulkGit(t, repo, "worktree", "add", "-b", "argus/settled", wt)
				task = &model.Task{Name: "settled", Project: "settled", Branch: "argus/settled", Worktree: wt, Status: model.StatusInProgress}
				testutil.NoError(t, d.Add(task))
				task.SetStatus(model.StatusPending)
				task.StartedAt = time.Time{}
				testutil.NoError(t, d.Update(task))
			}
			s.deleteOps = nil
			doc := seedDeleteJob(t, d, "settled", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
			testutil.NoError(t, s.processDeleteTask("settled", &doc, &doc.Results[0]))
			testutil.Equal(t, doc.Results[0].State, deleteTaskSucceeded)
			testutil.Equal(t, doc.Results[0].RecordDeleted, true)
			_, err := d.Get(task.ID)
			testutil.ErrorIs(t, err, db.ErrTaskNotFound)
			_, err = os.Stat(task.Worktree)
			testutil.True(t, os.IsNotExist(err))
		})
	}
}

func TestBulkDelete_OnlyRelevantRepositoryFailuresBlock(t *testing.T) {
	for _, claim := range []string{"unrelated", "branch", "base", "base origin", "base heads", "base remotes"} {
		t.Run(claim, func(t *testing.T) {
			s, d, _, stops, _ := bulkSetup(t)
			repo := t.TempDir()
			bulkGit(t, repo, "init", "-b", "master")
			bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
			testutil.NoError(t, d.SetProject("target", config.Project{Path: repo}))
			testutil.NoError(t, d.SetProject("missing", config.Project{Path: filepath.Join(t.TempDir(), "missing")}))
			wt, branch := agent.WorktreeDir("target", "remove"), "argus/remove"
			bulkGit(t, repo, "worktree", "add", "-b", branch, wt)
			target := &model.Task{Name: "target", Project: "target", Branch: branch, Worktree: wt, Status: model.StatusInReview}
			testutil.NoError(t, d.Add(target))
			survivor := &model.Task{Name: "survivor", Project: "missing", Branch: "argus/other", Status: model.StatusInReview}
			if claim == "branch" {
				survivor.Branch = branch
			}
			switch claim {
			case "base":
				survivor.BaseBranch = branch
			case "base origin":
				survivor.BaseBranch = "origin/" + branch
			case "base heads":
				survivor.BaseBranch = "refs/heads/" + branch
			case "base remotes":
				survivor.BaseBranch = "refs/remotes/origin/" + branch
			}
			testutil.NoError(t, d.Add(survivor))
			s.deleteOps = nil
			doc := seedDeleteJob(t, d, "relevance", storedDeleteResult{deleteResultView: deleteResultView{TaskID: target.ID, State: deleteTaskPending}})
			testutil.NoError(t, s.processDeleteTask("relevance", &doc, &doc.Results[0]))
			if claim == "unrelated" {
				testutil.Equal(t, doc.Results[0].State, deleteTaskSucceeded)
				_, err := os.Stat(wt)
				testutil.True(t, os.IsNotExist(err))
				cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
				cmd.Dir = repo
				testutil.True(t, cmd.Run() != nil)
			} else {
				testutil.Equal(t, doc.Results[0].State, deleteTaskFailed)
				testutil.Equal(t, doc.Results[0].RecordDeleted, false)
				_, err := d.Get(target.ID)
				testutil.NoError(t, err)
				_, err = os.Stat(wt)
				testutil.NoError(t, err)
				testutil.Equal(t, len(stops.stopped), 0)
			}
		})
	}
}

func TestBulkDelete_LockSetupFailurePreservesRecord(t *testing.T) {
	s, d, _, stops, _ := bulkSetup(t)
	task := addTask(t, d, "lock-failure")
	data := filepath.Join(os.Getenv("HOME"), ".argus")
	testutil.NoError(t, os.MkdirAll(data, 0700))
	testutil.NoError(t, os.WriteFile(filepath.Join(data, "locks"), []byte("obstruct"), 0600))
	doc := seedDeleteJob(t, d, "lock-failure", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	testutil.NoError(t, s.processDeleteTask("lock-failure", &doc, &doc.Results[0]))
	testutil.Equal(t, doc.Results[0].State, deleteTaskFailed)
	testutil.Equal(t, doc.Results[0].RecordDeleted, false)
	_, err := d.Get(task.ID)
	testutil.NoError(t, err)
	_, err = os.Stat(task.Worktree)
	testutil.NoError(t, err)
	testutil.Equal(t, len(stops.stopped), 0)
}
