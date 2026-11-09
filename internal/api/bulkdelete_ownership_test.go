package api

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/synctest"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type ownershipRunner struct{ agent.SessionProvider }
type ownershipSession struct{ agent.SessionHandle }

func (ownershipSession) PID() int { return 0 }
func (ownershipRunner) Start(*model.Task, config.Config, uint16, uint16, bool) (agent.SessionHandle, error) {
	return ownershipSession{}, nil
}

func bulkGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@local", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestBulkDelete_CreateBeforePersistOwnership(t *testing.T) {
	s, d, ops, _, _ := bulkSetup(t)
	repo := t.TempDir()
	bulkGit(t, repo, "init", "-b", "master")
	bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
	testutil.NoError(t, d.SetProject("race", config.Project{Path: repo, Branch: "HEAD"}))
	testutil.NoError(t, d.SetBackend("test", config.Backend{Command: "echo"}))
	wt := agent.WorktreeDir("race", "reuse")
	res := storedDeleteResult{deleteResultView: deleteResultView{TaskID: "old", RecordDeleted: true}, Snap: &deleteSnap{Worktree: wt, Branch: "argus/reuse", RepoDir: repo}}
	var survivor *model.Task
	synctest.Test(t, func(t *testing.T) {
		created, persist, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() {
			defer close(finished)
			var err error
			survivor, _, err = agent.CreateAndStart(d, ownershipRunner{}, agent.CreateInput{Project: "race", Name: "reuse", Backend: "test", OnWorktreeCreated: func(string) error { close(created); <-persist; return nil }})
			testutil.NoError(t, err)
		}()
		<-created
		done := make(chan struct{})
		go func() { defer close(done); s.cleanupDeletedTask(&res, ops) }()
		synctest.Wait()
		// The old job must wait while a fresh worktree has no persisted owner.
		ops.mu.Lock()
		testutil.Equal(t, len(ops.cleanups), 0)
		ops.mu.Unlock()
		close(persist)
		<-finished
		<-done
		s.deleteJobCancel()
	})
	testutil.True(t, survivor != nil)
	testutil.DeepEqual(t, ops.cleanups, []string{"|"})
	_, err := os.Stat(wt)
	testutil.NoError(t, err)
	bulkGit(t, repo, "rev-parse", "--verify", "refs/heads/argus/reuse")
}

func TestBulkDelete_RealBranchOwnership(t *testing.T) {
	for _, claim := range []string{"branch", "base branch", "alias", "legacy branch", "linked source", "case alias"} {
		t.Run(claim, func(t *testing.T) {
			s, d, _, _, _ := bulkSetup(t)
			repo, origin := t.TempDir(), filepath.Join(t.TempDir(), "origin.git")
			bulkGit(t, repo, "init", "-b", "master")
			bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
			bulkGit(t, repo, "init", "--bare", origin)
			bulkGit(t, repo, "remote", "add", "origin", origin)
			testutil.NoError(t, d.SetProject("real", config.Project{Path: repo}))
			wt := agent.WorktreeDir("real", "parent")
			branch := "argus/parent"
			bulkGit(t, repo, "worktree", "add", "-b", branch, wt)
			bulkGit(t, repo, "push", "origin", branch)
			survivor := &model.Task{Name: "child", Project: "real", Status: model.StatusInProgress, Branch: "argus/child"}
			switch claim {
			case "branch", "legacy branch":
				survivor.Branch = branch
			case "base branch", "linked source":
				survivor.BaseBranch = branch
			case "alias":
				survivor.Worktree = wt + "/."
			case "case alias":
				survivor.Worktree = filepath.Join(filepath.Dir(wt), "PARENT")
				if _, err := os.Stat(survivor.Worktree); os.IsNotExist(err) {
					t.Skip("filesystem is case sensitive")
				}
			}
			if claim == "linked source" {
				source := filepath.Join(t.TempDir(), "source")
				bulkGit(t, repo, "worktree", "add", "-b", "source", source)
				testutil.NoError(t, d.SetProject("real", config.Project{Path: source}))
			}
			testutil.NoError(t, d.Add(survivor))
			res := storedDeleteResult{deleteResultView: deleteResultView{TaskID: "old", RecordDeleted: true}, Snap: &deleteSnap{Worktree: wt, Branch: branch, RepoDir: repo}}
			if claim == "legacy branch" {
				res.Snap.Branch = "master"
			}
			s.cleanupDeletedTask(&res, gitDeleteOps{ctx: s.deleteContext()})
			testutil.Equal(t, res.State, deleteTaskSucceeded)
			testutil.Equal(t, res.LocalBranch, agent.CleanupNotApplicable)
			testutil.Equal(t, res.RemoteBranch, agent.CleanupNotApplicable)
			if res.Error == nil {
				t.Fatal("missing ownership protection reason")
			}
			testutil.Equal(t, *res.Error, "ownership_protected")
			bulkGit(t, repo, "rev-parse", "--verify", "refs/heads/"+branch)
			bulkGit(t, repo, "--git-dir", origin, "rev-parse", "--verify", "refs/heads/"+branch)
			if claim == "alias" || claim == "case alias" {
				_, err := os.Stat(wt)
				testutil.NoError(t, err)
			}
		})
	}
}

func TestBulkDelete_RealGitDefaultOps(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		t.Run(fmt.Sprint(dirty), func(t *testing.T) {
			s, d, _, _, _ := bulkSetup(t)
			s.deleteOps = nil
			repo := t.TempDir()
			bulkGit(t, repo, "init", "-b", "master")
			bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
			testutil.NoError(t, d.SetProject("default", config.Project{Path: repo}))
			wt := agent.WorktreeDir("default", "default")
			branch := "argus/default"
			bulkGit(t, repo, "worktree", "add", "-b", branch, wt)
			if dirty {
				testutil.NoError(t, os.WriteFile(filepath.Join(wt, "dirty"), []byte("keep"), 0600))
			}
			task := &model.Task{Name: "default", Project: "default", Branch: branch, Worktree: wt, Status: model.StatusInReview}
			testutil.NoError(t, d.Add(task))
			v := waitJob(t, s, submit(t, s, `{"task_ids":["`+task.ID+`"]}`))
			if dirty {
				testutil.Equal(t, v.Results[0].State, deleteTaskSkipped)
				_, err := os.Stat(wt)
				testutil.NoError(t, err)
			} else {
				testutil.Equal(t, v.Results[0].State, deleteTaskSucceeded)
				_, err := os.Stat(wt)
				testutil.True(t, os.IsNotExist(err))
			}
		})
	}
}

func TestBulkDelete_CleanupHoldsOwnershipThroughRemoval(t *testing.T) {
	s, _, _, _, _ := bulkSetup(t)
	repo := t.TempDir()
	bulkGit(t, repo, "init", "-b", "master")
	bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
	synctest.Test(t, func(t *testing.T) {
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		ops := observingDeleteOps{func(string, string, string) agent.GitCleanupResult {
			close(entered)
			<-release
			return agent.GitCleanupResult{}
		}}
		res := storedDeleteResult{deleteResultView: deleteResultView{TaskID: "old", RecordDeleted: true}, Snap: &deleteSnap{RepoDir: repo}}
		go func() { defer close(done); s.cleanupDeletedTask(&res, ops) }()
		<-entered
		acquired := make(chan struct{})
		go func() {
			unlock, err := agent.LockTaskGitOwnership(s.deleteContext(), repo)
			testutil.NoError(t, err)
			if unlock != nil {
				defer unlock()
			}
			close(acquired)
		}()
		synctest.Wait()
		select {
		case <-acquired:
			t.Error("creation passed the fence while cleanup was removing resources")
		default:
		}
		close(release)
		<-done
		<-acquired
		s.deleteJobCancel()
	})
}
