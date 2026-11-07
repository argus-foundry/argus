package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestReconcileHeraWorkerToInProgress(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status model.Status
		ready  bool
		want   bool
	}{
		{"stranded", model.StatusInReview, false, true}, {"accepted", model.StatusComplete, false, false}, {"closed out", model.StatusInReview, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := heraTestDB(t)
			task := &model.Task{Name: "worker", Status: tc.status}
			testutil.NoError(t, d.Add(task))
			o := mkOrch(t, d, "o")
			r := mkRole(t, d, o.ID, "worker", HeraKindWorker)
			mkBinding(t, d, r.ID, task.ID, "/wt")
			if tc.ready {
				testutil.NoError(t, d.SetMeta(task.ID, HeraMetaNamespace, HeraMetaKeyReadyToClose, "true"))
			}
			changed, err := d.ReconcileHeraWorkerToInProgress(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, changed, tc.want)
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			want := tc.status
			if tc.want {
				want = model.StatusInProgress
			}
			testutil.Equal(t, got.Status, want)
		})
	}
}

func TestSetStatus_ConditionalPreservesAcceptance(t *testing.T) {
	d := testDB(t)
	task := &model.Task{Name: "worker", Status: model.StatusComplete}
	testutil.NoError(t, d.Add(task))
	sink := &recordingSink{}
	previous := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(previous) })
	expected := model.StatusInReview
	changed, err := d.setStatus(task.ID, model.StatusInProgress, &expected)
	testutil.NoError(t, err)
	testutil.Equal(t, changed, false)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusComplete)
	testutil.Equal(t, len(sink.types()), 0)
}
