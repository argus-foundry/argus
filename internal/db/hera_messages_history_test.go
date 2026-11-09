package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHeraMessagesHistoryTimestampSkew(t *testing.T) {
	d := heraTestDB(t)
	o := mkOrch(t, d, "o")
	from := mkRole(t, d, o.ID, "from", HeraKindCoordinator)
	to := mkRole(t, d, o.ID, "to", HeraKindWorker)
	_, err := d.CreateHeraBinding(CreateHeraBindingInput{RoleID: to.ID, ArgusTaskID: "task", WorktreePath: "/wt"})
	testutil.NoError(t, err)
	older := mkMsg(t, d, from.ID, to.ID, "older", "older", nil)
	newer := mkMsg(t, d, from.ID, to.ID, "newer", "newer", nil)
	_, err = d.conn.Exec(`UPDATE hera_messages SET sent_at=CASE WHEN id=? THEN '2030-01-01T00:00:00Z' ELSE '2020-01-01T00:00:00Z' END`, older.ID)
	testutil.NoError(t, err)
	first, more, err := d.HeraMessagesPageForTask("task", 1, 0)
	testutil.NoError(t, err)
	testutil.Equal(t, len(first), 1)
	testutil.Equal(t, more, true)
	if len(first) != 1 {
		return
	}
	testutil.Equal(t, first[0].ID, newer.ID)
	next, more, err := d.HeraMessagesPageForTask("task", 1, first[0].ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(next), 1)
	testutil.Equal(t, more, false)
	if len(next) != 1 {
		return
	}
	testutil.Equal(t, next[0].ID, older.ID)
	testutil.Equal(t, next[0].SentAt.After(first[0].SentAt), true)
}
