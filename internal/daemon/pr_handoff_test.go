package daemon

import (
	"context"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// handedOffFixture archives a worker as handed off (PR open) and returns it.
func handedOffFixture(t *testing.T, d *Daemon) (taskID string, worker, coord *db.HeraRole) {
	t.Helper()
	seedTask(t, d.db, "ho-task", "argus/ho", false)
	worker, coord = seedHeraWorkerWithCoordinator(t, d.db, "O", "ho-task")
	_, err := d.db.ArchiveHeraUnit(db.ArchiveUnitInput{RoleID: worker.ID, Kind: db.ArchiveKindHandedOff, PRURL: "https://example/pr/1"})
	testutil.NoError(t, err)
	got, _ := d.db.Get("ho-task")
	testutil.Equal(t, got.Archived, true)
	return "ho-task", worker, coord
}

func fetchReturning(res gitutil.PRResult, calls *[]string) func(context.Context, string, map[string]string) (map[string]gitutil.PRResult, int, error) {
	return func(_ context.Context, _ string, branches map[string]string) (map[string]gitutil.PRResult, int, error) {
		out := map[string]gitutil.PRResult{}
		for b := range branches {
			if calls != nil {
				*calls = append(*calls, b)
			}
			out[b] = res
		}
		return out, 1, nil
	}
}

func TestPollPR_HandedOffArchivedTaskIsWatched(t *testing.T) {
	d, _ := testDaemon(t)
	taskID, _, coord := handedOffFixture(t, d)
	var calls []string
	d.prResolveRepo = resolveAll("drn/argus")

	// Open PR: polled, nothing settles, no board item.
	d.prBatchFetch = fetchReturning(gitutil.PRResult{State: model.PRApproved, URL: "https://example/pr/1"}, &calls)
	d.pollPRStatesOnce(context.Background())
	testutil.DeepEqual(t, calls, []string{"argus/ho"})
	open, _ := d.db.OpenHandoffRecords()
	testutil.Equal(t, len(open), 1)
	items, _ := d.db.ListHeraBoardItems(coord.ID, false)
	testutil.Equal(t, len(items), 0)

	// Merged: record settles, task completes, board fyi, no role-mail notices.
	d.prBatchFetch = fetchReturning(gitutil.PRResult{State: model.PRMergedClosed, URL: "https://example/pr/1", Merged: true}, nil)
	d.pollPRStatesOnce(context.Background())
	open, _ = d.db.OpenHandoffRecords()
	testutil.Equal(t, len(open), 0)
	got, _ := d.db.Get(taskID)
	testutil.Equal(t, got.Status, model.StatusComplete)
	testutil.Equal(t, got.Archived, true)
	items, _ = d.db.ListHeraBoardItems(coord.ID, false)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Kind, "fyi")
	in, _ := d.db.HeraInbox(coord.ID)
	testutil.Equal(t, len(in), 0) // no merge-notice mail for an archived role
}

func TestPollPR_HandedOffClosedUnmergedRevivesAndPostsDecision(t *testing.T) {
	d, _ := testDaemon(t)
	taskID, worker, coord := handedOffFixture(t, d)
	d.prResolveRepo = resolveAll("drn/argus")
	d.prBatchFetch = fetchReturning(gitutil.PRResult{State: model.PRMergedClosed, URL: "https://example/pr/1", Merged: false}, nil)
	d.pollPRStatesOnce(context.Background())

	got, _ := d.db.Get(taskID)
	testutil.Equal(t, got.Archived, false)
	testutil.Equal(t, got.Status, model.StatusInProgress)
	role, _ := d.db.HeraRole(worker.ID)
	testutil.Nil(t, role.ArchivedAt)
	items, _ := d.db.ListHeraBoardItems(coord.ID, false)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Kind, "decision")
	// A second poll is a no-op (the task is live again and the record settled).
	d.pollPRStatesOnce(context.Background())
	items, _ = d.db.ListHeraBoardItems(coord.ID, false)
	testutil.Equal(t, len(items), 1)
}

func TestPollPR_SettledForcedAndMergedArchivesAreNotPolled(t *testing.T) {
	d, _ := testDaemon(t)
	for _, kind := range []db.ArchiveKind{db.ArchiveKindForced, db.ArchiveKindMerged} {
		id := "t-" + string(kind)
		seedTask(t, d.db, id, "argus/"+string(kind), false)
		w, _ := seedHeraWorkerWithCoordinator(t, d.db, "O-"+string(kind), id)
		if kind == db.ArchiveKindMerged {
			testutil.NoError(t, d.db.SetStatus(id, model.StatusComplete))
		}
		_, err := d.db.ArchiveHeraUnit(db.ArchiveUnitInput{RoleID: w.ID, Kind: kind, Reason: "r"})
		testutil.NoError(t, err)
	}
	var calls []string
	d.prResolveRepo = resolveAll("drn/argus")
	d.prBatchFetch = fetchReturning(gitutil.PRResult{State: model.PRApproved, URL: "u"}, &calls)
	d.pollPRStatesOnce(context.Background())
	testutil.Equal(t, len(calls), 0)
}

func TestHandedOffEligible(t *testing.T) {
	rec := map[string]db.ArchiveRecord{"a": {TaskID: "a"}}
	testutil.Equal(t, handedOffEligible(&model.Task{ID: "a", Archived: true, Branch: "b"}, rec), true)
	testutil.Equal(t, handedOffEligible(&model.Task{ID: "a", Archived: false, Branch: "b"}, rec), false)
	testutil.Equal(t, handedOffEligible(&model.Task{ID: "a", Archived: true}, rec), false)
	testutil.Equal(t, handedOffEligible(&model.Task{ID: "x", Archived: true, Branch: "b"}, rec), false)
	testutil.Equal(t, handedOffEligible(nil, rec), false)
}

func TestSettleHandoffFromPR_IgnoresOpenAndUnknown(t *testing.T) {
	d, _ := testDaemon(t)
	taskID, _, _ := handedOffFixture(t, d)
	d.settleHandoffFromPR(taskID, gitutil.PRResult{State: model.PRDraft})
	d.settleHandoffFromPR(taskID, gitutil.PRResult{State: model.PRUnknown})
	open, _ := d.db.OpenHandoffRecords()
	testutil.Equal(t, len(open), 1)
}

func TestPollManualPR_HandedOffArchiveSettlesOnMerge(t *testing.T) {
	d, _ := testDaemon(t)
	taskID, _, coord := handedOffFixture(t, d)
	testutil.NoError(t, d.db.SetManualPR(taskID, manualURL))
	d.prURLFetch = func(context.Context, string) (gitutil.PRResult, error) {
		return gitutil.PRResult{State: model.PRMergedClosed, URL: manualURL, Merged: true}, nil
	}
	d.pollManualPRsOnce(context.Background())
	open, _ := d.db.OpenHandoffRecords()
	testutil.Equal(t, len(open), 0)
	items, _ := d.db.ListHeraBoardItems(coord.ID, false)
	testutil.Equal(t, len(items), 1)
}
