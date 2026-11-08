package db

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/drn/argus/internal/testutil"
)

func TestBoardKindOnlyEditEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.sql")
	d, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	observer, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = observer.Close() })
	o := mkOrch(t, d, "o")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: c.ID, ArgusTaskID: "task", WorktreePath: "/wt"})
	testutil.NoError(t, err)
	item, err := d.PostHeraBoardItem(c.ID, "decision", "Pick", "body")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ReadHeraBoardItemAt(c.ID, item.ID, item.UpdatedAt))
	sink := sinkFor(t)
	updated, err := d.UpdateHeraBoardItem(c.ID, item.ID, "fyi", item.Subject, item.Body)
	testutil.NoError(t, err)
	testutil.Equal(t, updated.Kind, "fyi")
	testutil.Equal(t, updated.Read, false)
	evs := sink.board()
	testutil.Equal(t, len(evs), 1)
	if len(evs) != 1 {
		return
	}
	var payload struct {
		RoleID    int64  `json:"role_id"`
		ItemID    int64  `json:"item_id"`
		Unread    int    `json:"unread_count"`
		Operation string `json:"operation"`
	}
	testutil.NoError(t, json.Unmarshal(evs[0].Payload, &payload))
	testutil.Equal(t, evs[0].TaskID, "task")
	testutil.Equal(t, payload.RoleID, c.ID)
	testutil.Equal(t, payload.ItemID, item.ID)
	testutil.Equal(t, payload.Operation, "updated")
	var unread int
	testutil.NoError(t, observer.conn.QueryRow(`SELECT COUNT(*) FROM hera_board_items WHERE role_id=? AND resolved=0 AND is_read=0`, c.ID).Scan(&unread))
	testutil.Equal(t, unread, 1)
	testutil.Equal(t, payload.Unread, unread)
}
