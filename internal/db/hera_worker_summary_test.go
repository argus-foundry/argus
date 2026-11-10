package db

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHeraWorkerMessageSummary(t *testing.T) {
	d := heraTestDB(t)
	o := mkOrch(t, d, "team")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	w := mkRole(t, d, o.ID, "worker", HeraKindWorker)
	other := mkRole(t, d, o.ID, "other", HeraKindWorker)
	line, unread, err := d.HeraWorkerMessageSummary(w.ID, c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, line, "")
	testutil.Equal(t, unread, 0)
	old := mkMsg(t, d, w.ID, c.ID, "old unread body", "old unread", nil)
	mkMsg(t, d, other.ID, c.ID, "other worker", "must not appear", nil)
	// More recent read history than a normal history page contains must not
	// hide the old unread message. Seed directly to avoid the send rate cap.
	now := formatTime(time.Now().Add(time.Minute))
	_, err = d.conn.Exec(`WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<501) INSERT INTO hera_messages(from_role_id,to_role_id,body,tldr,sent_at,read_at) SELECT ?,?,'read body','newest status',?,? FROM seq`, w.ID, c.ID, now, now)
	testutil.NoError(t, err)
	line, unread, err = d.HeraWorkerMessageSummary(w.ID, c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, line, "newest status")
	testutil.Equal(t, unread, 1)
	inbox, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(inbox), 2) // reading a summary never acknowledges mail
	_, err = d.MarkHeraMessagesRead(c.ID, []int64{old.ID})
	testutil.NoError(t, err)
	_, unread, err = d.HeraWorkerMessageSummary(w.ID, c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, unread, 0)
}
