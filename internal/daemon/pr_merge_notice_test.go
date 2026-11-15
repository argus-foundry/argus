package daemon

import (
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestMergeNoticeFinishedSnapshot(t *testing.T) {
	for _, state := range []string{"complete snapshot", "stale snapshot", "archived"} {
		t.Run(state, func(t *testing.T) {
			d, _ := testDaemon(t)
			seedTask(t, d.db, "worker-task", "argus/worker", false)
			worker, coord := seedHeraWorkerWithCoordinator(t, d.db, "o", "worker-task")
			snapshot, err := d.db.Get("worker-task")
			testutil.NoError(t, err)
			if state == "archived" {
				testutil.NoError(t, d.db.SetArchived(snapshot.ID, true))
			} else {
				testutil.NoError(t, d.db.SetStatus(snapshot.ID, model.StatusComplete))
			}
			if state == "complete snapshot" {
				snapshot, err = d.db.Get(snapshot.ID)
				testutil.NoError(t, err)
			}
			d.notifyRoleOfMergedPR(snapshot, "https://example/pr/1")
			d.notifyCoordinatorOfMergedPR(snapshot, "https://example/pr/1")
			inbox, err := d.db.HeraInbox(worker.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(inbox), 0)
			inbox, err = d.db.HeraInbox(coord.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(inbox), 1)
		})
	}
}
