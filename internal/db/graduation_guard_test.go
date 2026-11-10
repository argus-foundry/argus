package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestGraduationPendingBlocksArchiveAndDelete(t *testing.T) {
	d, b := graduationFixture(t)
	_, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	id := b.ArgusTaskID
	testutil.True(t, d.GraduationPending(id))

	assertUntouched := func(t *testing.T) {
		t.Helper()
		task, err := d.Get(id)
		testutil.NoError(t, err)
		testutil.False(t, task.Archived)
		meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
		testutil.NoError(t, err)
		testutil.Equal(t, meta[id][HeraMetaKeyGraduationPendingRestart], "true")
		live, err := d.HeraLiveBindingByTask(id)
		testutil.NoError(t, err)
		testutil.True(t, live != nil)
	}

	t.Run("SetArchived refused", func(t *testing.T) {
		testutil.ErrorIs(t, d.SetArchived(id, true), ErrGraduationPending)
		assertUntouched(t)
	})
	t.Run("Update archive refused", func(t *testing.T) {
		task, err := d.Get(id)
		testutil.NoError(t, err)
		task.SetArchived(true)
		testutil.ErrorIs(t, d.Update(task), ErrGraduationPending)
		assertUntouched(t)
	})
	t.Run("Delete refused", func(t *testing.T) {
		testutil.ErrorIs(t, d.Delete(id), ErrGraduationPending)
		assertUntouched(t)
	})
	t.Run("PruneTasks keeps it", func(t *testing.T) {
		pruned, skipped, err := d.PruneTasks([]string{id})
		testutil.NoError(t, err)
		testutil.Equal(t, len(pruned), 0)
		testutil.Equal(t, skipped, 1)
		assertUntouched(t)
	})

	testutil.NoError(t, d.CompleteHeraGraduation(id, "orientation"))
	testutil.False(t, d.GraduationPending(id))
	t.Run("allowed after the restart completes", func(t *testing.T) {
		testutil.NoError(t, d.SetArchived(id, true))
		testutil.NoError(t, d.SetArchived(id, false))
		testutil.NoError(t, d.Delete(id))
	})
}

func TestGraduationPendingUnarchiveUnaffected(t *testing.T) {
	d, b := graduationFixture(t)
	_, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetArchived(b.ArgusTaskID, false))
}
