package db

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHeraMessagesHistoryIndexPlan(t *testing.T) {
	d := heraTestDB(t)
	o := mkOrch(t, d, "o")
	from := mkRole(t, d, o.ID, "from", HeraKindCoordinator)
	ended := mkRole(t, d, o.ID, "ended", HeraKindWorker)
	active := mkRole(t, d, o.ID, "active", HeraKindWorker)
	other := mkRole(t, d, o.ID, "other", HeraKindWorker)
	binding, err := d.CreateHeraBinding(CreateHeraBindingInput{RoleID: ended.ID, ArgusTaskID: "task", WorktreePath: "/ended"})
	testutil.NoError(t, err)
	testutil.NoError(t, d.EndHeraBinding(binding.ID, "moved"))
	_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: active.ID, ArgusTaskID: "task", WorktreePath: "/active"})
	testutil.NoError(t, err)
	_, err = d.conn.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<5101)
 INSERT INTO hera_messages(from_role_id,to_role_id,body,tldr,sent_at,read_at)
 SELECT ?, CASE WHEN i>101 THEN ? WHEN i%2=0 THEN ? ELSE ? END, 'body', 'tldr', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z' FROM n`, from.ID, other.ID, ended.ID, active.ID)
	testutil.NoError(t, err)
	for _, tc := range []struct {
		name        string
		cursor      int64
		first, last int64
	}{
		{"first page", 0, 52, 101},
		{"cursor page", 52, 2, 51},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, args := heraMessagesPageQuery("task", 50, tc.cursor)
			plan := explainPlan(t, d, query, args...)
			t.Log(plan)
			testutil.Contains(t, plan, "USING INDEX idx_hera_msg_history (to_role_id=?")
			testutil.Equal(t, strings.Contains(plan, "SCAN hera_messages"), false)
			if tc.cursor > 0 {
				testutil.Contains(t, plan, "to_role_id=? AND id<?")
			}
			messages, more, err := d.HeraMessagesPageForTask("task", 50, tc.cursor)
			testutil.NoError(t, err)
			testutil.Equal(t, len(messages), 50)
			testutil.Equal(t, more, true)
			for index, message := range messages {
				testutil.Equal(t, message.ID, tc.first+int64(index))
				testutil.Equal(t, message.ToRoleID == ended.ID || message.ToRoleID == active.ID, true)
			}
			testutil.Equal(t, messages[49].ID, tc.last)
		})
	}
}

func TestHeraMessagesHistoryIndexReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sql")
	d, err := Open(path)
	testutil.NoError(t, err)
	o := mkOrch(t, d, "o")
	from := mkRole(t, d, o.ID, "from", HeraKindCoordinator)
	to := mkRole(t, d, o.ID, "to", HeraKindWorker)
	m := mkMsg(t, d, from.ID, to.ID, "body", "tldr", nil)
	_, err = d.conn.Exec(`DROP INDEX IF EXISTS idx_hera_msg_history`)
	testutil.NoError(t, err)
	testutil.NoError(t, d.Close())
	reopened, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	var definition string
	testutil.NoError(t, reopened.conn.QueryRow(`SELECT sql FROM sqlite_master WHERE name='idx_hera_msg_history'`).Scan(&definition))
	testutil.Contains(t, definition, "hera_messages(to_role_id, id)")
	testutil.Equal(t, strings.Contains(strings.ToUpper(definition), "WHERE"), false)
	messages, err := reopened.HeraMessagesByIDs([]int64{m.ID})
	testutil.NoError(t, err)
	testutil.Equal(t, len(messages), 1)
	testutil.Equal(t, messages[0].Body, "body")
}
