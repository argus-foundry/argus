package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// The watcher's completion step is a compare-and-set on the observed phase:
// a human abandon (+archive) between the sweep's snapshot and completion must
// create no orientation/message and must not un-archive the task.
func TestCompleteHeraGraduationAt(t *testing.T) {
	t.Run("normal completion writes pid, status, orientation", func(t *testing.T) {
		d, b := graduationFixture(t)
		_, err := d.GraduateHeraWorker(b.ID)
		testutil.NoError(t, err)
		id := b.ArgusTaskID
		ok, err := d.AdvanceGraduationPhase(id, "true", "restart:5")
		testutil.NoError(t, err)
		testutil.True(t, ok)
		done, err := d.CompleteHeraGraduationAt(id, "restart:5", "orientation", 99)
		testutil.NoError(t, err)
		testutil.True(t, done)
		got, err := d.Get(id)
		testutil.NoError(t, err)
		testutil.Equal(t, got.AgentPID, 99)
		testutil.Equal(t, got.Status, model.StatusInProgress)
		meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
		testutil.NoError(t, err)
		testutil.Equal(t, meta[id]["graduation_orientation"], "orientation")
		testutil.Equal(t, meta[id][HeraMetaKeyGraduationPendingRestart], "")
		msgs, err := d.Inbox(id, InboxFilter{})
		testutil.NoError(t, err)
		testutil.Equal(t, len(msgs), 1)
	})
	t.Run("abandon plus archive in the window creates nothing", func(t *testing.T) {
		d, b := graduationFixture(t)
		_, err := d.GraduateHeraWorker(b.ID)
		testutil.NoError(t, err)
		id := b.ArgusTaskID
		testutil.NoError(t, d.AbandonGraduationPending(id))
		testutil.NoError(t, d.SetArchived(id, true))
		done, err := d.CompleteHeraGraduationAt(id, "restart:5", "orientation", 99)
		testutil.NoError(t, err)
		testutil.False(t, done)
		got, err := d.Get(id)
		testutil.NoError(t, err)
		testutil.True(t, got.Archived)
		testutil.Equal(t, got.AgentPID, 0)
		meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
		testutil.NoError(t, err)
		testutil.Equal(t, meta[id]["graduation_orientation"], "")
		msgs, err := d.Inbox(id, InboxFilter{})
		testutil.NoError(t, err)
		testutil.Equal(t, len(msgs), 0)
	})
	t.Run("changed phase refuses", func(t *testing.T) {
		d, b := graduationFixture(t)
		_, err := d.GraduateHeraWorker(b.ID)
		testutil.NoError(t, err)
		done, err := d.CompleteHeraGraduationAt(b.ArgusTaskID, "restart:5", "orientation", 99)
		testutil.NoError(t, err)
		testutil.False(t, done)
		testutil.True(t, d.GraduationPending(b.ArgusTaskID))
	})
	t.Run("task row gone clears the marker and creates nothing else", func(t *testing.T) {
		d, b := graduationFixture(t)
		_, err := d.GraduateHeraWorker(b.ID)
		testutil.NoError(t, err)
		id := b.ArgusTaskID
		_, err = d.conn.Exec(`DELETE FROM tasks WHERE id=?`, id)
		testutil.NoError(t, err)
		done, err := d.CompleteHeraGraduationAt(id, "true", "orientation", 99)
		testutil.NoError(t, err)
		testutil.False(t, done)
		meta, err := d.ListMetaByNamespace(HeraMetaNamespace)
		testutil.NoError(t, err)
		testutil.Equal(t, meta[id]["graduation_orientation"], "")
		// The marker delete commits: a dead task must not keep the watcher looping.
		testutil.False(t, d.GraduationPending(id))
		msgs, err := d.Inbox(id, InboxFilter{})
		testutil.NoError(t, err)
		testutil.Equal(t, len(msgs), 0)
	})
}
