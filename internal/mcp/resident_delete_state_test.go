package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func residentStateGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// Commits on a detached HEAD and a paused rebase/merge/cherry-pick are not
// on the branch and not dirt in the tree, yet die with the worktree: a
// non-force delete refuses them before the stop, naming the reason.
func TestResidentDelete_WorktreeStateRefusedBeforeStop(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, repo, wt string)
		want  string
	}{
		{"detached head with a commit", func(t *testing.T, _, wt string) {
			residentStateGit(t, wt, "checkout", "--detach")
			residentStateGit(t, wt, "commit", "--allow-empty", "-m", "on a detached head")
		}, "HEAD is detached"},
		{"other branch", func(t *testing.T, _, wt string) {
			residentStateGit(t, wt, "checkout", "-b", "side")
			residentStateGit(t, wt, "commit", "--allow-empty", "-m", "on side")
		}, "HEAD is on side, not the worker's branch"},
		{"rebase in progress", func(t *testing.T, _, wt string) {
			residentStateGit(t, wt, "commit", "--allow-empty", "-m", "to rebase")
			cmd := exec.Command("git", "rebase", "--exec", "false", "HEAD~1")
			cmd.Dir = wt
			if err := cmd.Run(); err == nil {
				t.Fatal("rebase should have paused at the failing exec")
			}
		}, "a rebase is in progress"},
		{"merge in progress", func(t *testing.T, repo, wt string) {
			residentStateGit(t, repo, "commit", "--allow-empty", "-m", "on master")
			residentStateGit(t, wt, "merge", "--no-ff", "--no-commit", "master")
		}, "a merge is in progress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d, task, role, runner, caller := busyDeleteFixture(t, true)
			repo := d.Config().Projects["p"].Path
			tc.setup(t, repo, task.Worktree)
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				return mergesafety.Verdict{Safe: true}, nil
			}
			text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
			testutil.True(t, isErr)
			testutil.Contains(t, text, "not safe: ")
			testutil.Contains(t, text, tc.want)
			testutil.Equal(t, runner.stopCount(), 0)
			assertWorkerKept(t, d, task, role)
		})
	}
}

// A detached-HEAD commit made while the delete classifies is caught by the
// post-stop re-check.
func TestResidentDelete_DetachedHeadDuringClassificationKept(t *testing.T) {
	s, d, task, role, runner, caller := busyDeleteFixture(t, true)
	s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
		residentStateGit(t, task.Worktree, "checkout", "--detach")
		residentStateGit(t, task.Worktree, "commit", "--allow-empty", "-m", "late detached commit")
		return mergesafety.Verdict{Safe: true}, nil
	}
	text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
	testutil.True(t, isErr)
	testutil.Contains(t, text, "not deleted")
	testutil.Contains(t, text, "session was stopped")
	testutil.Contains(t, text, "HEAD is detached")
	testutil.Equal(t, runner.stopCount(), 1)
	assertWorkerKept(t, d, task, role)
}

// Another cleanup holding the repo's cleanup lock (which RemoveWorktreeAndBranch
// holds across an untimed origin push) past the budget refuses "host busy;
// retry" before the stop, with nothing changed; the refusal is in slog.
func TestResidentDelete_RepoLockBusyBeforeStop(t *testing.T) {
	logs, _ := captureLogs(t)
	useFakeResidentClock(t)
	s, d, task, role, runner, caller := busyDeleteFixture(t, true)
	s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
		return mergesafety.Verdict{Safe: true}, nil
	}
	repo := d.Config().Projects["p"].Path
	held, released := make(chan struct{}), make(chan struct{})
	go func() {
		unlock, err := agent.LockRepoContext(context.Background(), repo)
		if err != nil {
			t.Error(err)
			close(held)
			return
		}
		close(held)
		// Old code queued on the lock after the nuke; let it go eventually
		// so a regression fails instead of hanging.
		select {
		case <-released:
		case <-time.After(5 * time.Second):
		}
		unlock()
	}()
	<-held
	text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
	close(released)
	testutil.True(t, isErr)
	testutil.Contains(t, text, "host busy; retry")
	testutil.Contains(t, text, "held its lock")
	testutil.Equal(t, runner.stopCount(), 0)
	testutil.True(t, runner.HasSession(task.ID))
	assertWorkerKept(t, d, task, role)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInProgress)
	testutil.Contains(t, logs.String(), "host busy (repo cleanup lock")
	testutil.Contains(t, logs.String(), "refused: host busy (another worktree cleanup")
}

// Budget exhausted DURING the pre-stop work (not before it): the idle check,
// the work summary and the classifier each report "host busy; retry" with
// their own reason, nothing stopped, and never as dirt or "not safe".
func TestResidentDelete_HostBusyInFlight(t *testing.T) {
	for _, tc := range []struct {
		name string
		// margin is what is left before the first-change deadline once the
		// queued handler starts.
		margin time.Duration
		idle   bool
		setup  func(t *testing.T, s *Server, sess *busyFakeSession)
		want   string
	}{
		{"idle check", 2 * time.Second, false, func(t *testing.T, s *Server, sess *busyFakeSession) {
			old := residentIdleSampling
			t.Cleanup(func() { residentIdleSampling = old })
			residentIdleSampling = prodIdleSampling
			n := 0
			sess.tail = func() []byte {
				n++
				return []byte(fmt.Sprintf("compiling package %s\r\n", strings.Repeat("x", n)))
			}
		}, "the idle check ran out"},
		{"work summary", 300 * time.Millisecond, true, func(t *testing.T, s *Server, _ *busyFakeSession) {
			old := residentSummarize
			t.Cleanup(func() { residentSummarize = old })
			residentSummarize = func(ctx context.Context, cfg config.Config, tk *model.Task, pr string) mergesafety.WorkSummary {
				<-ctx.Done()
				return old(ctx, cfg, tk, pr)
			}
		}, "the work summary ran out"},
		{"classifier", 2 * time.Second, true, func(t *testing.T, s *Server, _ *busyFakeSession) {
			old := residentSummarize
			t.Cleanup(func() { residentSummarize = old })
			residentSummarize = func(_ context.Context, cfg config.Config, tk *model.Task, pr string) mergesafety.WorkSummary {
				return old(context.Background(), cfg, tk, pr)
			}
			s.residentClassify = func(ctx context.Context, _ *model.Task) (mergesafety.Verdict, error) {
				<-ctx.Done()
				return mergesafety.Verdict{}, ctx.Err()
			}
		}, "the merge-safety check ran out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := useFakeResidentClock(t)
			s, d, task, role, runner, caller := busyDeleteFixture(t, tc.idle)
			s.residentClassify = func(context.Context, *model.Task) (mergesafety.Verdict, error) {
				return mergesafety.Verdict{Safe: true}, nil
			}
			tc.setup(t, s, runner.sessions[task.ID])
			residentWorkerMutationMu.Lock()
			done := make(chan struct{})
			var text string
			var isErr bool
			go func() {
				defer close(done)
				text, isErr = postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work"}`)
			}()
			for clk.nowCalls() == 0 {
				time.Sleep(time.Millisecond)
			}
			clk.Advance(residentDeleteDeadline - residentPostStopReserve() - tc.margin)
			residentWorkerMutationMu.Unlock()
			<-done
			testutil.True(t, isErr)
			testutil.Contains(t, text, "host busy; retry")
			testutil.Contains(t, text, tc.want)
			if strings.Contains(text, "dirty files") || strings.Contains(text, "not safe") {
				t.Fatalf("budget expiry reported as dirt or unsafe: %s", text)
			}
			testutil.Equal(t, runner.stopCount(), 0)
			assertWorkerKept(t, d, task, role)
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Status, model.StatusInProgress)
			_, err = os.Stat(task.Worktree)
			testutil.NoError(t, err)
		})
	}
}
