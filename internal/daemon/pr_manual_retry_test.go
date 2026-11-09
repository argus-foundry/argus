package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestPollManualPR_TerminalRetryCadence(t *testing.T) {
	for _, mode := range []string{"fetch-error", "lookup-error", "verified", "recovery"} {
		t.Run(mode, func(t *testing.T) {
			d, _ := testDaemon(t)
			task := &model.Task{ID: "legacy-terminal", Name: "old", Status: model.StatusComplete, CreatedAt: time.Now().Add(-8 * 24 * time.Hour)}
			testutil.NoError(t, d.db.Add(task))
			testutil.NoError(t, d.db.SetManualPR(task.ID, manualURL))
			testutil.NoError(t, d.db.SetMetaBatch(task.ID, "pr", map[string]string{"state": "merged-closed", "url": manualURL, "lifecycle": "open"}))
			calls := 0
			d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
				calls++
				if mode == "fetch-error" || mode == "lookup-error" || (mode == "recovery" && calls < 3) {
					return gitutil.PRResult{}, errors.New(mode)
				}
				return gitutil.PRResult{State: model.PRMergedClosed, URL: manualURL, Merged: true}, nil
			}
			expected := 1
			testutil.Equal(t, prCadenceSelects(1, task.ID, 30), false)
			for cycle := uint64(1); cycle <= 60; cycle++ {
				if prCadenceSelects(cycle, task.ID, 30) {
					expected++
				}
				d.pollCycle = cycle
				d.pollManualPRsOnce(context.Background())
				if cycle == 1 {
					testutil.Equal(t, calls, 1)
				}
			}
			if mode == "verified" {
				expected = 1
			}
			testutil.Equal(t, calls, expected)
			if mode == "fetch-error" || mode == "lookup-error" {
				testutil.Equal(t, storedLifecycle(t, d, task.ID), "open")
			} else {
				testutil.Equal(t, storedLifecycle(t, d, task.ID), "merged")
			}
		})
	}
}

func TestPollManualPR_MergeNotices(t *testing.T) {
	for _, state := range []string{"approved", "merged-closed"} {
		for _, lifecycle := range []string{"", "open"} {
			t.Run(state+"/"+lifecycle, func(t *testing.T) {
				d, _ := testDaemon(t)
				seedTask(t, d.db, "worker-task", "argus/worker", false)
				worker, coord := seedHeraWorkerWithCoordinator(t, d.db, "O", "worker-task")
				testutil.NoError(t, d.db.SetManualPR("worker-task", manualURL))
				testutil.NoError(t, d.db.SetMetaBatch("worker-task", "pr", map[string]string{"state": state, "url": manualURL, "lifecycle": lifecycle}))
				d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
					return gitutil.PRResult{State: model.PRMergedClosed, URL: manualURL, Merged: true}, nil
				}
				sink := installPRSink(t)
				d.pollManualPRsOnce(context.Background())
				d.pollManualPRsOnce(context.Background())
				testutil.Equal(t, len(sink.prChanged()), 1)
				want := 0
				if state == "approved" {
					want = 1
				}
				wm, err := d.db.HeraInbox(worker.ID)
				testutil.NoError(t, err)
				cm, err := d.db.HeraInbox(coord.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, len(wm), want)
				testutil.Equal(t, len(cm), want)
			})
		}
	}
}
