package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestPollPR_TerminalVerificationRetryCadence(t *testing.T) {
	for _, mode := range []string{"fetch-error", "missing-result", "no-pr", "verified", "recovery", "missing-recovery"} {
		t.Run(mode, func(t *testing.T) {
			d, _ := testDaemon(t)
			task := &model.Task{ID: "legacy-terminal", Name: "old", Project: "p", Branch: "old", Status: model.StatusComplete, CreatedAt: time.Now().Add(-8 * 24 * time.Hour)}
			testutil.NoError(t, d.db.Add(task))
			testutil.NoError(t, d.db.SetMetaBatch(task.ID, "pr", map[string]string{"state": "merged-closed", "url": "https://github.com/o/r/pull/3"}))
			d.prResolveRepo = resolveAll("o/r")
			calls := 0
			d.prBatchFetch = func(context.Context, string, map[string]string) (map[string]gitutil.PRResult, int, error) {
				calls++
				switch mode {
				case "fetch-error":
					return nil, 0, fmt.Errorf("repository unavailable or rate limited")
				case "recovery", "missing-recovery":
					if calls < 3 {
						if mode == "missing-recovery" {
							return map[string]gitutil.PRResult{}, 1, nil
						}
						return nil, 0, fmt.Errorf("temporary failure")
					}
					return map[string]gitutil.PRResult{"old": {State: model.PRMergedClosed, Merged: true}}, 1, nil
				case "missing-result":
					return map[string]gitutil.PRResult{}, 1, nil
				case "no-pr":
					return map[string]gitutil.PRResult{"old": {State: model.PRNone}}, 1, nil
				default:
					return map[string]gitutil.PRResult{"old": {State: model.PRMergedClosed, Merged: true}}, 1, nil
				}
			}
			testutil.Equal(t, prCadenceSelects(1, task.ID, 30), false)
			cadenceFetches := 0
			for cycle := uint64(1); cycle <= 60; cycle++ {
				if prCadenceSelects(cycle, task.ID, 30) {
					cadenceFetches++
				}
				d.pollCycle = cycle
				d.pollPRStatesOnce(context.Background())
				if cycle == 1 {
					testutil.Equal(t, calls, 1)
				}
			}
			want := cadenceFetches + 1
			if mode == "verified" {
				want = 1
			}
			switch mode {
			case "fetch-error", "missing-result":
				testutil.Equal(t, storedLifecycle(t, d, task.ID), "")
				entries, err := d.db.ListMeta(task.ID, "pr")
				testutil.NoError(t, err)
				got := map[string]string{}
				for _, e := range entries {
					got[e.Key] = e.Value
				}
				testutil.DeepEqual(t, got, map[string]string{"state": "merged-closed", "url": "https://github.com/o/r/pull/3"})
			case "no-pr":
				testutil.Equal(t, storedLifecycle(t, d, task.ID), "unknown")
			default:
				testutil.Equal(t, storedLifecycle(t, d, task.ID), "merged")
			}
			t.Logf("60 cycles: mode=%s fetches=%d lifecycle=%s", mode, calls, storedLifecycle(t, d, task.ID))
			testutil.Equal(t, calls, want)
		})
	}
}

func TestPollPR_FirstVerificationPerTask(t *testing.T) {
	d, _ := testDaemon(t)
	d.prResolveRepo = resolveAll("o/r")
	var branchesFetched []string
	d.prBatchFetch = func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
		for branch := range branches {
			branchesFetched = append(branchesFetched, branch)
		}
		return nil, 0, fmt.Errorf("unavailable")
	}
	for _, id := range []string{"legacy-terminal", "another-terminal"} {
		task := &model.Task{ID: id, Name: id, Project: "p", Branch: id, Status: model.StatusComplete, CreatedAt: time.Now().Add(-8 * 24 * time.Hour)}
		testutil.NoError(t, d.db.Add(task))
		testutil.NoError(t, d.db.SetMetaBatch(id, "pr", map[string]string{"state": "merged-closed", "lifecycle": "open", "url": "https://github.com/o/r/pull/3"}))
	}
	d.pollCycle = 1
	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, len(branchesFetched), 2)
}
