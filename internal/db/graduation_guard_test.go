package db

import (
	"errors"
	"strconv"
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

func TestGraduationPendingFailsClosed(t *testing.T) {
	d, b := graduationFixture(t)
	_, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	id := b.ArgusTaskID
	testutil.NoError(t, d.Close())

	pending, err := d.GraduationPendingErr(id)
	testutil.True(t, err != nil)
	testutil.False(t, pending)
	// Fail-closed wrapper: an unreadable state reads as pending.
	testutil.True(t, d.GraduationPending(id))
	gerr := graduationPendingGuard(d.conn, id)
	testutil.True(t, gerr != nil)
	testutil.False(t, errors.Is(gerr, ErrGraduationPending))
}

func TestAbandonGraduationPending(t *testing.T) {
	d, b := graduationFixture(t)
	res, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	id := b.ArgusTaskID
	meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[id][HeraMetaKeyGraduationBindingID], strconv.FormatInt(res.NewBinding.ID, 10))
	testutil.NoError(t, d.SetMeta(id, HeraMetaNamespace, "graduation_orientation", "body"))

	testutil.NoError(t, d.AbandonGraduationPending(id))
	meta, err = d.ListMetaByNamespace(HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[id][HeraMetaKeyGraduationPendingRestart], "")
	testutil.Equal(t, meta[id]["graduation_orientation"], "")
	testutil.Equal(t, meta[id][HeraMetaKeyGraduationBindingID], "")
	testutil.Equal(t, meta[id]["graduated_from"], ResidentOrchestratorName)
	testutil.False(t, d.GraduationPending(id))
	testutil.NoError(t, d.SetArchived(id, true))
}

func TestCompleteHeraGraduationClearsBindingID(t *testing.T) {
	d, b := graduationFixture(t)
	_, err := d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	testutil.NoError(t, d.CompleteHeraGraduation(b.ArgusTaskID, "orientation"))
	meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
	testutil.NoError(t, err)
	testutil.Equal(t, meta[b.ArgusTaskID][HeraMetaKeyGraduationBindingID], "")
}

func TestAdvanceGraduationPhase_CAS(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	read := func() string {
		meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
		testutil.NoError(t, err)
		return meta["t1"][HeraMetaKeyGraduationPendingRestart]
	}
	// Key absent: refuses and does NOT create it.
	ok, err := d.AdvanceGraduationPhase("t1", "true", "restart:5")
	testutil.NoError(t, err)
	testutil.False(t, ok)
	testutil.Equal(t, read(), "")
	testutil.NoError(t, d.SetMeta("t1", HeraMetaNamespace, HeraMetaKeyGraduationPendingRestart, "true"))
	// Stale expected phase refuses.
	ok, err = d.AdvanceGraduationPhase("t1", "restart:1", "restart:5")
	testutil.NoError(t, err)
	testutil.False(t, ok)
	testutil.Equal(t, read(), "true")
	ok, err = d.AdvanceGraduationPhase("t1", "true", "restart:5")
	testutil.NoError(t, err)
	testutil.True(t, ok)
	testutil.Equal(t, read(), "restart:5")
	// Same-value retry still reports success.
	ok, err = d.AdvanceGraduationPhase("t1", "restart:5", "restart:5")
	testutil.NoError(t, err)
	testutil.True(t, ok)
	// Abandon then advance: stays cleared.
	testutil.NoError(t, d.AbandonGraduationPending("t1"))
	ok, err = d.AdvanceGraduationPhase("t1", "restart:5", "restart:6")
	testutil.NoError(t, err)
	testutil.False(t, ok)
	testutil.Equal(t, read(), "")
}
