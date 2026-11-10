package mcp

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/testutil"
)

// A forced delete returns its summary once the worker's own stop and reclaim
// are done; the fleet-wide reclaim sweep runs afterwards in the background.
func TestResidentDelete_SummaryDoesNotWaitOnFleetReconcile(t *testing.T) {
	s, d, res, o := residentHeraFixture(t)
	worker, role, _ := residentTestWorker(t, d, o.ID, "worker")
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.residentFleetReconcile = func(*db.DB, hera.SessionChecker) (hera.Summary, error) {
		close(started)
		<-release
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
