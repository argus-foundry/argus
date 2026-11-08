package db

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type boardObservingSink struct{ observe func(model.Event) }

func (s boardObservingSink) Emit(e model.Event) {
	if e.Type == model.EventTypeHeraBoardChanged {
		s.observe(e)
	}
}

func TestBoardEventsObserveCommittedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.sql")
	d, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	observer, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = observer.Close() })
	o := mkOrch(t, d, "o")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	oldBinding, err := d.CreateHeraBinding(CreateHeraBindingInput{RoleID: c.ID, ArgusTaskID: "old-task", WorktreePath: "/old"})
	testutil.NoError(t, err)
	testutil.NoError(t, d.EndHeraBinding(oldBinding.ID, "moved"))
	_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: c.ID, ArgusTaskID: "t1", WorktreePath: "/wt"})
	testutil.NoError(t, err)
	var op string
	var unread int
	var itemID int64
	var wantRead, wantResolved bool
	var noticeCount int
	seen := 0
	prev := events.SetSink(boardObservingSink{observe: func(e model.Event) {
		seen++
		locked := d.mu.TryLock()
		testutil.Equal(t, locked, true)
		if locked {
			d.mu.Unlock()
		}
		var payload struct {
			RoleID int64  `json:"role_id"`
			ItemID int64  `json:"item_id"`
			Unread int    `json:"unread_count"`
			Op     string `json:"operation"`
		}
		testutil.NoError(t, json.Unmarshal(e.Payload, &payload))
		testutil.Equal(t, e.TaskID, "t1")
		testutil.Equal(t, payload.RoleID, c.ID)
		testutil.Equal(t, payload.Op, op)
		testutil.Equal(t, payload.Unread, unread)
		testutil.Equal(t, len(mapPayload(t, e.Payload)), 4)
		if itemID != 0 {
			testutil.Equal(t, payload.ItemID, itemID)
		}
		n, _, err := observer.HeraBoardUnread(c.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, payload.Unread, n)
		i, err := scanBoard(observer.conn.QueryRow(`SELECT `+boardColumns+` FROM hera_board_items WHERE role_id=? AND id=?`, c.ID, payload.ItemID))
		testutil.NoError(t, err)
		testutil.Equal(t, i.Read, wantRead)
		testutil.Equal(t, i.Resolved, wantResolved)
		var notices int
		testutil.NoError(t, observer.conn.QueryRow(`SELECT COUNT(*) FROM hera_messages WHERE from_role_id=to_role_id`).Scan(&notices))
		testutil.Equal(t, notices, noticeCount)
	}})
	t.Cleanup(func() { events.SetSink(prev) })
	op, unread = "posted", 1
	i, err := d.PostHeraBoardItem(c.ID, "decision", "Pick", "body")
	testutil.NoError(t, err)
	itemID = i.ID
	op, unread, wantRead = "read_state", 0, true
	testutil.NoError(t, d.ReadHeraBoardItemAt(c.ID, i.ID, i.UpdatedAt))
	op, unread, wantRead = "updated", 1, false
	i, err = d.UpdateHeraBoardItem(c.ID, i.ID, "decision", "Pick", "edited")
	testutil.NoError(t, err)
	op, unread, wantRead = "read_state", 0, true
	_, _, err = d.SetHeraBoardItemRead(c.ID, i.ID, true, i.UpdatedAt)
	testutil.NoError(t, err)
	op, unread, wantRead = "read_state", 1, false
	_, _, err = d.SetHeraBoardItemRead(c.ID, i.ID, false, i.UpdatedAt)
	testutil.NoError(t, err)
	op, unread, wantResolved, noticeCount = "resolved", 0, true, 1
	_, _, err = d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, "stop")
	testutil.NoError(t, err)
	testutil.Equal(t, seen, 6)
}

func mapPayload(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	testutil.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestBoardEventsSuppressFailedDismissal(t *testing.T) {
	for _, tc := range []struct{ name, setup string }{
		{"write", `CREATE TRIGGER reject_notice BEFORE INSERT ON hera_messages BEGIN SELECT RAISE(ABORT, 'notice failed'); END`},
		{"commit", `CREATE TABLE commit_guard (role_id INTEGER REFERENCES hera_roles(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER reject_commit AFTER INSERT ON hera_messages BEGIN INSERT INTO commit_guard VALUES(-1); END`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, c, i := boardFixture(t)
			_, err := d.conn.Exec(tc.setup)
			testutil.NoError(t, err)
			sink := sinkFor(t)
			_, _, err = d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, "stop")
			testutil.Error(t, err)
			testutil.Equal(t, len(sink.board()), 0)
			items, err := d.ListHeraBoardItems(c.ID, false)
			testutil.NoError(t, err)
			testutil.Equal(t, len(items), 1)
			testutil.Equal(t, items[0].Read, false)
			messages, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(messages), 0)
		})
	}
}

func TestDismissHeraBoardItemNoticeReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.sql")
	d, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	o := mkOrch(t, d, "o")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	i, err := d.PostHeraBoardItem(c.ID, "decision", "Pick", "body")
	testutil.NoError(t, err)
	_, _, err = d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, "original note")
	testutil.NoError(t, err)
	inbox, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(inbox), 1)
	noticeID := inbox[0].ID
	testutil.NoError(t, d.Close())
	d, err = Open(path)
	testutil.NoError(t, err)
	pending, err := d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 0)
	_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: c.ID, ArgusTaskID: "t1", WorktreePath: "/wt"})
	testutil.NoError(t, err)
	for range 2 {
		_, _, err = d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, "original note")
		testutil.NoError(t, err)
		pending, err = d.PendingHeraBoardNotices()
		testutil.NoError(t, err)
		testutil.Equal(t, len(pending), 1)
		testutil.Equal(t, pending[0].ID, noticeID)
		testutil.Equal(t, pending[0].DeliveryMode, HeraDeliveryPending)
		testutil.Contains(t, pending[0].Body, "original note")
		testutil.Equal(t, pending[0].FromRoleID, c.ID)
		testutil.Equal(t, pending[0].ToRoleID, c.ID)
		testutil.Nil(t, pending[0].ReadAt)
	}
}
