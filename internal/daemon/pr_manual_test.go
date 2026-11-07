package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/drn/argus/internal/gitutil"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

const manualURL = "https://github.com/o/r/pull/9"

func storedPR(t *testing.T, d *Daemon, id string) map[string]string {
	t.Helper()
	entries, err := d.db.ListMeta(id, "pr")
	testutil.NoError(t, err)
	out := map[string]string{}
	for _, e := range entries {
		out[e.Key] = e.Value
	}
	return out
}

func branchPollerReturning(d *Daemon, res gitutil.PRResult) {
	d.prBatchFetch = func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
		out := map[string]gitutil.PRResult{}
		for b := range branches {
			out[b] = res
		}
		return out, 1, nil
	}
	d.prResolveRepo = resolveAll("o/r")
}

func TestPollPR_ManualLinkSurvivesConflictingBranchResult(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "m1", "argus/shared", false)
	seedTask(t, d.db, "m2", "argus/shared", false)
	testutil.NoError(t, d.db.SetManualPR("m1", manualURL))
	d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
		return gitutil.PRResult{State: model.PRApproved, URL: manualURL}, nil
	}
	branchPollerReturning(d, gitutil.PRResult{State: model.PRAwaitingReview, URL: "https://github.com/o/r/pull/1"})

	d.pollManualPRsOnce(context.Background())
	d.pollPRStatesOnce(context.Background())
	d.pollPRStatesOnce(context.Background())

	m1 := storedPR(t, d, "m1")
	testutil.Equal(t, m1["url"], manualURL)
	testutil.Equal(t, m1["state"], "approved")
	link, err := d.db.ManualPR("m1")
	testutil.NoError(t, err)
	testutil.Equal(t, link, manualURL)
	m2 := storedPR(t, d, "m2")
	testutil.Equal(t, m2["url"], "https://github.com/o/r/pull/1")
	testutil.Equal(t, m2["state"], "awaiting-review")
}

func TestPollPR_ManualLinkSurvivesRestart(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "r1", "argus/r1", false)
	testutil.NoError(t, d.db.SetManualPR("r1", manualURL))
	d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
		return gitutil.PRResult{State: model.PRAwaitingReview, URL: manualURL}, nil
	}
	branchPollerReturning(d, gitutil.PRResult{State: model.PRNone})
	d.pollManualPRsOnce(context.Background())

	d.pollPRStatesOnce(context.Background())
	link, err := d.db.ManualPR("r1")
	testutil.NoError(t, err)
	testutil.Equal(t, link, manualURL)
	testutil.Equal(t, storedPR(t, d, "r1")["url"], manualURL)
}

func TestPollManualPR_EmitsManualEventAndMergeOutcome(t *testing.T) {
	sink := installPRSink(t)
	d, _ := testDaemon(t)
	seedTask(t, d.db, "e1", "argus/e1", false)
	testutil.NoError(t, d.db.SetManualPR("e1", manualURL))
	d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
		return gitutil.PRResult{State: model.PRMergedClosed, URL: manualURL, Merged: true}, nil
	}

	d.pollManualPRsOnce(context.Background())

	testutil.Equal(t, storedPR(t, d, "e1")["lifecycle"], "merged")
	evs := sink.prChanged()
	testutil.Equal(t, len(evs), 1)
	var payload map[string]string
	testutil.NoError(t, json.Unmarshal(evs[0].Payload, &payload))
	testutil.DeepEqual(t, payload, map[string]string{
		"url": manualURL, "state": "merged-closed", "lifecycle": "merged", "source": "manual",
	})

	d.pollManualPRsOnce(context.Background())
	testutil.Equal(t, len(sink.prChanged()), 1)
}

func TestPollManualPR_LookupErrorKeepsAssociationAndCache(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "x1", "argus/x1", false)
	testutil.NoError(t, d.db.SetManualPR("x1", manualURL))
	d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
		return gitutil.PRResult{State: model.PRApproved, URL: manualURL}, nil
	}
	d.pollManualPRsOnce(context.Background())
	d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
		return gitutil.PRResult{}, errors.New("gh down")
	}
	d.pollManualPRsOnce(context.Background())

	link, err := d.db.ManualPR("x1")
	testutil.NoError(t, err)
	testutil.Equal(t, link, manualURL)
	testutil.Equal(t, storedPR(t, d, "x1")["state"], "approved")
}

func TestPollManualPR_ClearedLinkIsNotRewritten(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "c1", "argus/c1", false)
	testutil.NoError(t, d.db.SetManualPR("c1", manualURL))
	d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
		testutil.NoError(t, d.db.ClearManualPR("c1"))
		return gitutil.PRResult{State: model.PRApproved, URL: manualURL}, nil
	}
	d.pollManualPRsOnce(context.Background())
	testutil.Equal(t, len(storedPR(t, d, "c1")), 0)
}

func TestPollPR_ClearedLinkRestoresAutomaticDiscovery(t *testing.T) {
	d, _ := testDaemon(t)
	seedTask(t, d.db, "a1", "argus/a1", false)
	testutil.NoError(t, d.db.SetManualPR("a1", manualURL))
	branchPollerReturning(d, gitutil.PRResult{State: model.PRAwaitingReview, URL: "https://github.com/o/r/pull/2"})
	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, len(storedPR(t, d, "a1")), 0)

	testutil.NoError(t, d.db.ClearManualPR("a1"))
	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, storedPR(t, d, "a1")["url"], "https://github.com/o/r/pull/2")
}

func TestPollPR_UsesLiveHeadBranch(t *testing.T) {
	setup := func(t *testing.T, head func(string) (gitutil.Head, error)) (*Daemon, *[]string) {
		d, _ := testDaemon(t)
		seedTask(t, d.db, "a", "argus/recorded", false)
		d.prLiveHead = head
		var mu sync.Mutex
		queried := []string{}
		d.prBatchFetch = func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
			mu.Lock()
			defer mu.Unlock()
			out := map[string]gitutil.PRResult{}
			for b := range branches {
				queried = append(queried, b)
				out[b] = gitutil.PRResult{State: model.PRApproved, URL: "https://github.com/o/r/pull/2"}
			}
			return out, 1, nil
		}
		d.prResolveRepo = resolveAll("o/r")
		return d, &queried
	}

	t.Run("queries the checked-out branch, not the recorded one", func(t *testing.T) {
		live := "feature-live"
		d, queried := setup(t, func(string) (gitutil.Head, error) { return gitutil.Head{Branch: &live}, nil })
		d.pollPRStatesOnce(context.Background())
		testutil.DeepEqual(t, *queried, []string{"feature-live"})
		tk, err := d.db.Get("a")
		testutil.NoError(t, err)
		testutil.Equal(t, tk.Branch, "argus/recorded")
	})
	t.Run("detached head is not looked up", func(t *testing.T) {
		d, queried := setup(t, func(string) (gitutil.Head, error) { return gitutil.Head{Detached: true}, nil })
		d.pollPRStatesOnce(context.Background())
		testutil.Equal(t, len(*queried), 0)
		testutil.Equal(t, len(storedPR(t, d, "a")), 0)
	})
	t.Run("unreadable head is not replaced by the stored branch", func(t *testing.T) {
		d, queried := setup(t, func(string) (gitutil.Head, error) { return gitutil.Head{}, errors.New("boom") })
		d.pollPRStatesOnce(context.Background())
		testutil.Equal(t, len(*queried), 0)
	})
}
