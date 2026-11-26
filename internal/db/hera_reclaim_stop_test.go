package db

import (
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHeraReclaimStopPersistsForExactSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	tui, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = tui.Close() })
	daemon, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = daemon.Close() })

	task := &model.Task{ID: "reclaim", Name: "Reclaim", Status: model.StatusInProgress}
	testutil.NoError(t, tui.Add(task))
	armed, err := tui.ArmHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.False(t, armed)
	testutil.NoError(t, tui.SetArchived(task.ID, true))
	armed, err = tui.ArmHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.True(t, armed)

	matched, err := daemon.ConsumeHeraReclaimStop(task.ID, 42)
	testutil.NoError(t, err)
	testutil.False(t, matched)
	testutil.NoError(t, tui.ClearHeraReclaimStop(task.ID, 42))
	matched, err = daemon.ConsumeHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.True(t, matched)
	matched, err = daemon.ConsumeHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.False(t, matched)

	persisted, err := daemon.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, persisted.Status, model.StatusInProgress)
}

func TestHeraReclaimStopRespectsCurrentTaskState(t *testing.T) {
	d := testDB(t)
	task := &model.Task{ID: "reclaim", Name: "Reclaim", Status: model.StatusInProgress, Archived: true}
	testutil.NoError(t, d.Add(task))

	_, err := d.ArmHeraReclaimStop(task.ID, 0)
	testutil.Error(t, err)
	matched, err := d.ConsumeHeraReclaimStop(task.ID, 0)
	testutil.NoError(t, err)
	testutil.False(t, matched)

	armed, err := d.ArmHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.True(t, armed)
	testutil.NoError(t, d.SetArchived(task.ID, false))
	matched, err = d.ConsumeHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.False(t, matched)
	testutil.NoError(t, d.SetArchived(task.ID, true))
	matched, err = d.ConsumeHeraReclaimStop(task.ID, 41)
	testutil.NoError(t, err)
	testutil.False(t, matched)
}

func TestReconcileMissingSessionPreservesArmedReclaim(t *testing.T) {
	d := testDB(t)
	const pid = 41
	task := &model.Task{ID: "reclaim", Name: "Reclaim", AgentPID: pid, Status: model.StatusInProgress, Archived: true}
	testutil.NoError(t, d.Add(task))
	armed, err := d.ArmHeraReclaimStop(task.ID, pid)
	testutil.NoError(t, err)
	testutil.True(t, armed)

	changed, err := d.ReconcileMissingSession(task.ID, pid)
	testutil.NoError(t, err)
	testutil.False(t, changed)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInProgress)

	testutil.NoError(t, d.ClearHeraReclaimStop(task.ID, pid))
	changed, err = d.ReconcileMissingSession(task.ID, pid-1)
	testutil.NoError(t, err)
	testutil.False(t, changed)
	changed, err = d.ReconcileMissingSession(task.ID, pid)
	testutil.NoError(t, err)
	testutil.True(t, changed)
	got, err = d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Status, model.StatusInReview)
}
