package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestReconcileInterruptedSessions(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		status                                 model.Status
		prior, bound, alive, closing, archived bool
		want                                   bool
	}{
		{name: "pending plain recorded live", status: model.StatusPending, prior: true, want: true},
		{name: "review coordinator binding", status: model.StatusInReview, bound: true, want: true},
		{name: "pending binding", status: model.StatusPending, bound: true, want: true},
		{name: "progress fallback", status: model.StatusInProgress, want: true},
		{name: "plain stopped", status: model.StatusInReview},
		{name: "still alive", status: model.StatusInReview, prior: true, alive: true},
		{name: "closing", status: model.StatusInReview, prior: true, bound: true, closing: true},
		{name: "complete", status: model.StatusComplete, prior: true, bound: true},
		{name: "archived", status: model.StatusInProgress, prior: true, archived: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := db.OpenInMemory()
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			task := &model.Task{ID: "t", Name: "task", Status: tc.status}
			testutil.NoError(t, d.Add(task))
			if tc.archived {
				testutil.NoError(t, d.SetArchived(task.ID, true))
			}
			if tc.closing {
				testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
			}
			if tc.bound {
				o, err := d.CreateHeraOrchestrator("o", "")
				testutil.NoError(t, err)
				r, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator})
				testutil.NoError(t, err)
				_, err = d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: r.ID, ArgusTaskID: task.ID})
				testutil.NoError(t, err)
			}
			ids, err := ReconcileInterruptedSessions(d, map[string]bool{task.ID: tc.alive}, map[string]bool{task.ID: tc.prior})
			testutil.NoError(t, err)
			testutil.Equal(t, len(ids) == 1, tc.want)
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			if tc.want {
				testutil.Equal(t, got.Status, model.StatusInReview)
			} else {
				testutil.Equal(t, got.Status, tc.status)
			}
		})
	}
}
