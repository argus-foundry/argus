package daemon

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestPollPR_InFlightLinkSuppressesNotices(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "qa-race", "argus/race", false)
	worker, coord := seedHeraWorkerWithCoordinator(t, d.db, "qa", "qa-race")
	sink := installPRSink(t)
	d.prResolveRepo = resolveAll("o/r")
	d.prBatchFetch = func(context.Context, string, map[string]string) (map[string]gitutil.PRResult, int, error) {
		testutil.NoError(t, d.db.SetManualPR("qa-race", manualURL))
		return map[string]gitutil.PRResult{"argus/race": {State: model.PRMergedClosed, URL: "https://github.com/o/r/pull/1", Merged: true}}, 1, nil
	}
	d.pollPRStatesOnce(context.Background())
	link, e := d.db.ManualPR("qa-race")
	testutil.NoError(t, e)
	testutil.Equal(t, link, manualURL)
	testutil.Equal(t, len(storedPR(t, d, "qa-race")), 0)
	wm, e := d.db.HeraInbox(worker.ID)
	testutil.NoError(t, e)
	cm, e := d.db.HeraInbox(coord.ID)
	testutil.NoError(t, e)
	for _, m := range wm {
		t.Logf("worker notice %s", m.Body)
	}
	for _, m := range cm {
		t.Logf("coord notice %s", m.Body)
	}
	testutil.Equal(t, len(wm), 0)
	testutil.Equal(t, len(cm), 0)
	testutil.Equal(t, len(sink.prChanged()), 0)
}
func TestPollManualPR_PrunedLinkStopsPolling(t *testing.T) {
	for _, mode := range []string{"completed", "explicit"} {
		t.Run(mode, func(t *testing.T) {
			d, _ := testDaemon(t)
			seedTask(t, d.db, "qa-prune", "argus/prune", false)
			testutil.NoError(t, d.db.SetStatus("qa-prune", model.StatusComplete))
			testutil.NoError(t, d.db.SetManualPR("qa-prune", manualURL))
			if mode == "completed" {
				p, _, e := d.db.PruneCompleted()
				testutil.NoError(t, e)
				testutil.Equal(t, len(p), 1)
			} else {
				p, _, e := d.db.PruneTasks([]string{"qa-prune"})
				testutil.NoError(t, e)
				testutil.Equal(t, len(p), 1)
			}
			_, e := d.db.Get("qa-prune")
			testutil.Equal(t, e != nil, true)
			calls := 0
			d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
				calls++
				return gitutil.PRResult{State: model.PRApproved, URL: manualURL}, nil
			}
			d.pollManualPRsOnce(context.Background())
			d.pollManualPRsOnce(context.Background())
			links, e := d.db.ManualPRs()
			testutil.NoError(t, e)
			t.Logf("orphan links=%v fetches=%d orphan cache=%v", links, calls, storedPR(t, d, "qa-prune"))
			testutil.Equal(t, calls, 0)
			testutil.Equal(t, len(links), 0)
		})
	}
}

func TestPollManualPR_Eligibility(t *testing.T) {
	for _, mode := range []string{"archived", "orphan", "dormant-unknown"} {
		t.Run(mode, func(t *testing.T) {
			d, _ := testDaemon(t)
			task := &model.Task{ID: "old", Name: "old", CreatedAt: time.Now().Add(-30 * 24 * time.Hour)}
			if mode == "orphan" {
				testutil.NoError(t, d.db.WithTx(func(tx *sql.Tx) error {
					_, err := tx.Exec("INSERT INTO task_pr_links(task_id,url,created_at) VALUES(?,?,?)", task.ID, manualURL, time.Now().Format(time.RFC3339))
					return err
				}))
			} else {
				testutil.NoError(t, d.db.Add(task))
				testutil.NoError(t, d.db.SetManualPR(task.ID, manualURL))
				if mode == "archived" {
					testutil.NoError(t, d.db.SetArchived(task.ID, true))
				}
			}
			calls := 0
			d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
				calls++
				return gitutil.PRResult{}, errors.New("PR inaccessible")
			}
			for cycle := uint64(1); cycle <= 60; cycle++ {
				d.pollCycle = cycle
				d.pollManualPRsOnce(context.Background())
			}
			want := 0
			if mode == "dormant-unknown" {
				want = 2
			}
			testutil.Equal(t, calls, want)
			testutil.Equal(t, len(storedPR(t, d, task.ID)), 0)
		})
	}
}

func TestPollManualPR_ForeignTerminalCacheIsRefreshed(t *testing.T) {
	d, _ := testDaemon(t)
	task := &model.Task{ID: "legacy-terminal", Name: "old", CreatedAt: time.Now().Add(-30 * 24 * time.Hour)}
	testutil.NoError(t, d.db.Add(task))
	testutil.NoError(t, d.db.SetManualPR(task.ID, manualURL))
	testutil.NoError(t, d.db.SetMetaBatch(task.ID, "pr", map[string]string{"state": "merged-closed", "lifecycle": "merged", "url": "https://github.com/o/r/pull/1"}))
	calls := 0
	d.prURLFetch = func(_ context.Context, url string) (gitutil.PRResult, error) {
		calls++
		testutil.Equal(t, url, manualURL)
		return gitutil.PRResult{State: model.PRApproved, URL: url}, nil
	}
	d.pollCycle = 1
	d.pollManualPRsOnce(context.Background())
	testutil.Equal(t, calls, 1)
	testutil.DeepEqual(t, storedPR(t, d, task.ID), map[string]string{"state": "approved", "lifecycle": "open", "url": manualURL})
}
