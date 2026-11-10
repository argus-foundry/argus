package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// A forced delete returns its summary once the worker's own stop and reclaim
// are done; the fleet-wide reclaim sweep runs afterwards in the background.
func TestResidentDelete_SummaryDoesNotWaitOnFleetReconcile(t *testing.T) {
	s, d, res, o := residentHeraFixture(t)
	worker, role, _ := residentTestWorker(t, d, o.ID, "worker")
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	// The sweep must never run under the shared worker-mutation lock (it would
	// serialize every spawn/delete/graduate behind a fleet-wide scan). The
	// stub probes the lock only after release, i.e. after the tool handler
	// returned and dropped its own hold.
	sweepHeldMutationLock := true
	s.residentFleetReconcile = func(*db.DB, hera.SessionChecker) (hera.Summary, error) {
		close(started)
		<-release
		if residentWorkerMutationMu.TryLock() {
			sweepHeldMutationLock = false
			residentWorkerMutationMu.Unlock()
		}
		close(finished)
		return hera.Summary{}, nil
	}
	type result struct {
		text  string
		isErr bool
	}
	done := make(chan result, 1)
	go func() {
		text, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_delete_worker", `{"role":"worker","force":true}`)
		done <- result{text, isErr}
	}()
	var got result
	select {
	case got = <-done:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("tool result waited on the fleet-wide reclaim sweep")
	}
	testutil.False(t, got.isErr)
	testutil.Contains(t, got.text, "Deleted worker worker")
	testutil.Contains(t, got.text, "Unmerged summary (REPORT to Aaron now)")
	select {
	case <-finished:
		t.Fatal("sweep finished before release: it was not asynchronous")
	default:
	}
	// The worker's own reclaim already happened synchronously.
	r, err := d.HeraRole(role.ID)
	testutil.NoError(t, err)
	testutil.True(t, r.NukedAt != nil)
	_, err = d.Get(worker.ID)
	testutil.ErrorIs(t, err, db.ErrTaskNotFound)

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("fleet-wide reclaim sweep never ran")
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("fleet-wide reclaim sweep never finished")
	}
	s.residentBackground.Wait()
	testutil.False(t, sweepHeldMutationLock)
}

func TestResidentDelete_AsyncSweepPanicRecovered(t *testing.T) {
	s, d, res, o := residentHeraFixture(t)
	residentTestWorker(t, d, o.ID, "worker")
	ran := make(chan struct{})
	s.residentFleetReconcile = func(*db.DB, hera.SessionChecker) (hera.Summary, error) {
		close(ran)
		panic("boom")
	}
	_, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_delete_worker", `{"role":"worker","force":true}`)
	testutil.False(t, isErr)
	s.residentBackground.Wait()
	<-ran
}

// The deleted worker's branch is deleted on origin in the background, after
// the result, with its own timeout and a logged outcome; the synchronous part
// (worktree and local branch) is already done when the summary returns.
func TestResidentDelete_RemoteBranchDeleteAsync(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ux.log")
	testutil.NoError(t, uxlog.Init(logPath))
	t.Cleanup(uxlog.Close)
	s, _, task, _, _, caller := busyDeleteFixture(t, false)
	old := residentRemoteBranchDelete
	t.Cleanup(func() { residentRemoteBranchDelete = old })
	started, release := make(chan struct{}), make(chan struct{})
	var gotDir, gotBranch string
	var hadDeadline bool
	residentRemoteBranchDelete = func(ctx context.Context, dir, branch string) error {
		_, hadDeadline = ctx.Deadline()
		gotDir, gotBranch = dir, branch
		close(started)
		<-release
		return nil
	}
	done := make(chan string, 1)
	go func() {
		text, isErr := postTool(t, s, "/mcp?caller="+caller, "hera_delete_worker", `{"role":"work","force":true}`)
		if isErr {
			text = "ERROR: " + text
		}
		done <- text
	}()
	var text string
	select {
	case text = <-done:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("tool result waited on the origin branch delete")
	}
	testutil.Contains(t, text, "Deleted worker work")
	testutil.Contains(t, text, "Unmerged summary (REPORT to Aaron now)")
	// Local removal already happened.
	_, err := os.Stat(task.Worktree)
	testutil.True(t, os.IsNotExist(err))
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("origin branch delete never ran")
	}
	close(release)
	s.residentBackground.Wait()
	testutil.Equal(t, gotBranch, task.Branch)
	testutil.True(t, gotDir != "")
	testutil.True(t, hadDeadline)
	raw, err := os.ReadFile(logPath)
	testutil.NoError(t, err)
	testutil.Contains(t, string(raw), "[resident] delete: remote branch deleted task=work branch="+task.Branch)
}
