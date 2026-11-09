package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestPollPR_BackfillDoesNotRepeatMergeNotifications(t *testing.T) {
	for _, lifecycle := range []string{"", "open"} {
		t.Run("legacy-"+lifecycle, func(t *testing.T) {
			d, _ := testDaemon(t)
			seedTask(t, d.db, "worker-task", "argus/worker", false)
			worker, coord := seedHeraWorkerWithCoordinator(t, d.db, "O", "worker-task")
			url := "https://github.com/o/r/pull/1"
			testutil.NoError(t, d.db.SetMetaBatch("worker-task", "pr", map[string]string{"state": "merged-closed", "url": url, "lifecycle": lifecycle}))
			task, err := d.db.Get("worker-task")
			testutil.NoError(t, err)
			d.notifyCoordinatorOfMergedPR(task, url)
			d.notifyRoleOfMergedPR(task, url)
			d.prResolveRepo = resolveAll("o/r")
			d.prBatchFetch = func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
				out := map[string]gitutil.PRResult{}
				for b := range branches {
					out[b] = gitutil.PRResult{State: model.PRMergedClosed, URL: url, Merged: true}
				}
				return out, 1, nil
			}
			sink := installPRSink(t)
			d.pollPRStatesOnce(context.Background())
			d.pollPRStatesOnce(context.Background())
			testutil.Equal(t, len(sink.prChanged()), 1)
			testutil.Equal(t, storedLifecycle(t, d, "worker-task"), "merged")
			wm, err := d.db.HeraInbox(worker.ID)
			testutil.NoError(t, err)
			cm, err := d.db.HeraInbox(coord.ID)
			testutil.NoError(t, err)
			t.Logf("after lifecycle backfill worker notices=%d coordinator notices=%d", len(wm), len(cm))
			testutil.Equal(t, len(wm), 1)
			testutil.Equal(t, len(cm), 1)
		})
	}
}

func TestPollPR_RollbackOpenLifecycleRefreshed(t *testing.T) {
	for _, merged := range []bool{false, true} {
		t.Run(boolStrForLifecycle(merged), func(t *testing.T) {
			d, _ := testDaemon(t)
			seedTask(t, d.db, "rollback", "argus/rollback", false)
			url := "https://github.com/o/r/pull/7"
			testutil.NoError(t, d.db.SetMetaBatch("rollback", "pr", map[string]string{"state": "approved", "url": url, "lifecycle": "open"}))
			_, err := d.db.SetMetaBatchIfChanged("rollback", "pr", map[string]string{"state": "merged-closed", "url": url})
			testutil.NoError(t, err)
			testutil.Equal(t, model.ResolvePRLifecycle("merged-closed", storedLifecycle(t, d, "rollback")), model.PRLifecycleUnknown)
			calls := 0
			d.prResolveRepo = resolveAll("o/r")
			res := gitutil.PRResult{State: model.PRMergedClosed, URL: url, Merged: merged}
			d.prBatchFetch = func(context.Context, string, map[string]string) (map[string]gitutil.PRResult, int, error) {
				calls++
				return map[string]gitutil.PRResult{"argus/rollback": res}, 1, nil
			}
			d.pollPRStatesOnce(context.Background())
			d.pollPRStatesOnce(context.Background())
			testutil.Equal(t, calls, 1)
			testutil.Equal(t, storedLifecycle(t, d, "rollback"), string(res.Lifecycle()))
			testutil.Equal(t, model.ResolvePRLifecycle("merged-closed", storedLifecycle(t, d, "rollback")), res.Lifecycle())
		})
	}
}
func boolStrForLifecycle(merged bool) string {
	if merged {
		return "merged"
	}
	return "closed"
}
func TestPollPR_DormantTerminalNextPoll(t *testing.T) {
	d, _ := testDaemon(t)
	task := &model.Task{ID: "legacy-terminal", Name: "old", Project: "p", Branch: "old", Status: model.StatusComplete, CreatedAt: time.Now().Add(-8 * 24 * time.Hour)}
	testutil.NoError(t, d.db.Add(task))
	testutil.NoError(t, d.db.SetMetaBatch(task.ID, "pr", map[string]string{"state": "merged-closed", "url": "https://github.com/o/r/pull/3"}))
	d.prResolveRepo = resolveAll("o/r")
	calls := 0
	d.prBatchFetch = func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
		calls++
		out := map[string]gitutil.PRResult{}
		for b := range branches {
			out[b] = gitutil.PRResult{State: model.PRMergedClosed, URL: "https://github.com/o/r/pull/3", Merged: true}
		}
		return out, 1, nil
	}
	d.pollCycle = 1
	testutil.Equal(t, prCadenceSelects(1, task.ID, 30), false)
	d.pollPRStatesOnce(context.Background())
	t.Logf("first poll fetches=%d lifecycle=%q stride=%d", calls, storedLifecycle(t, d, task.ID), prPollCadenceStride(task, model.PRMergedClosed, time.Now()))
	testutil.Equal(t, calls, 1)
	testutil.Equal(t, storedLifecycle(t, d, task.ID), "merged")
	for cycle := uint64(2); cycle <= 13; cycle++ {
		d.pollCycle = cycle
		d.pollPRStatesOnce(context.Background())
	}
	testutil.Equal(t, calls, 1)
}
