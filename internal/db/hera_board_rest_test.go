package db

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type recordSink struct {
	mu  sync.Mutex
	evs []model.Event
}

func (r *recordSink) Emit(ev model.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, ev)
}

func (r *recordSink) board() []model.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.Event
	for _, e := range r.evs {
		if e.Type == model.EventTypeHeraBoardChanged {
			out = append(out, e)
		}
	}
	return out
}

func sinkFor(t *testing.T) *recordSink {
	t.Helper()
	s := &recordSink{}
	prev := events.SetSink(s)
	t.Cleanup(func() { events.SetSink(prev) })
	return s
}

func boardFixture(t *testing.T) (*DB, *HeraRole, *HeraBoardItem) {
	t.Helper()
	d := heraTestDB(t)
	o := mkOrch(t, d, "orch")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	_, err := d.CreateHeraBinding(CreateHeraBindingInput{RoleID: c.ID, ArgusTaskID: "t1", WorktreePath: "/wt"})
	testutil.NoError(t, err)
	i, err := d.PostHeraBoardItem(c.ID, "decision", "Pick", "body")
	testutil.NoError(t, err)
	return d, c, i
}

func TestSetHeraBoardItemRead(t *testing.T) {
	t.Run("read then unread keeps revision and emits", func(t *testing.T) {
		d, c, i := boardFixture(t)
		sink := sinkFor(t)
		got, n, err := d.SetHeraBoardItemRead(c.ID, i.ID, true, i.UpdatedAt)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Read, true)
		testutil.Equal(t, n, 0)
		testutil.Equal(t, formatTime(got.UpdatedAt), formatTime(i.UpdatedAt))
		got, n, err = d.SetHeraBoardItemRead(c.ID, i.ID, false, i.UpdatedAt)
		testutil.NoError(t, err)
		testutil.Equal(t, got.Read, false)
		testutil.Equal(t, n, 1)
		evs := sink.board()
		testutil.Equal(t, len(evs), 2)
		testutil.Equal(t, evs[0].TaskID, "t1")
		testutil.Contains(t, string(evs[0].Payload), `"operation":"read_state"`)
		testutil.Contains(t, string(evs[0].Payload), `"unread_count":0`)
		testutil.Contains(t, string(evs[1].Payload), `"unread_count":1`)
	})
	t.Run("idempotent read emits nothing", func(t *testing.T) {
		d, c, i := boardFixture(t)
		_, _, err := d.SetHeraBoardItemRead(c.ID, i.ID, true, i.UpdatedAt)
		testutil.NoError(t, err)
		sink := sinkFor(t)
		_, _, err = d.SetHeraBoardItemRead(c.ID, i.ID, true, i.UpdatedAt)
		testutil.NoError(t, err)
		testutil.Equal(t, len(sink.board()), 0)
	})
	t.Run("stale revision conflicts and stays unread", func(t *testing.T) {
		d, c, i := boardFixture(t)
		_, err := d.UpdateHeraBoardItem(c.ID, i.ID, "decision", "Pick", "changed")
		testutil.NoError(t, err)
		sink := sinkFor(t)
		_, _, err = d.SetHeraBoardItemRead(c.ID, i.ID, true, i.UpdatedAt)
		testutil.ErrorIs(t, err, ErrHeraBoardConflict)
		n, _, _ := d.HeraBoardUnread(c.ID)
		testutil.Equal(t, n, 1)
		testutil.Equal(t, len(sink.board()), 0)
	})
	t.Run("resolved and ownership", func(t *testing.T) {
		d, c, i := boardFixture(t)
		testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "agent", ""))
		_, _, err := d.SetHeraBoardItemRead(c.ID, i.ID, true, i.UpdatedAt)
		testutil.ErrorIs(t, err, ErrHeraBoardResolved)
		_, _, err = d.SetHeraBoardItemRead(c.ID+99, i.ID, true, i.UpdatedAt)
		testutil.ErrorIs(t, err, ErrHeraBoardNotFound)
	})
}

func TestDismissHeraBoardItem(t *testing.T) {
	t.Run("single durable notice across repeats", func(t *testing.T) {
		d, c, i := boardFixture(t)
		sink := sinkFor(t)
		got, n, err := d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, "no")
		testutil.NoError(t, err)
		testutil.Equal(t, got.Resolved, true)
		testutil.Equal(t, got.ResolvedBy, "human")
		testutil.Equal(t, n, 0)
		_, _, err = d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, "no")
		testutil.NoError(t, err)
		notices, err := d.PendingHeraBoardNotices()
		testutil.NoError(t, err)
		testutil.Equal(t, len(notices), 1)
		testutil.Equal(t, len(sink.board()), 1)
		testutil.Contains(t, string(sink.board()[0].Payload), `"operation":"resolved"`)
		_, _, err = d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, "different")
		testutil.ErrorIs(t, err, ErrHeraBoardResolved)
	})
	t.Run("notice survives missing binding", func(t *testing.T) {
		d, c, i := boardFixture(t)
		bs, err := d.EndHeraBindingsForTask("t1", "gone")
		testutil.NoError(t, err)
		testutil.Equal(t, bs, 1)
		_, _, err = d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, "")
		testutil.NoError(t, err)
		var count int
		testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM hera_messages WHERE from_role_id=to_role_id AND to_role_id=? AND read_at IS NULL`, c.ID).Scan(&count))
		testutil.Equal(t, count, 1)
	})
	t.Run("changed revision conflicts without notice", func(t *testing.T) {
		d, c, i := boardFixture(t)
		_, err := d.UpdateHeraBoardItem(c.ID, i.ID, "decision", "Pick", "new")
		testutil.NoError(t, err)
		_, _, err = d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, "")
		testutil.ErrorIs(t, err, ErrHeraBoardConflict)
		var count int
		testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM hera_messages WHERE from_role_id=to_role_id`).Scan(&count))
		testutil.Equal(t, count, 0)
	})
	t.Run("oversized note invalid", func(t *testing.T) {
		for _, tc := range []struct{ name, note string }{
			{"NUL bytes", string(make([]byte, 64*1024+1))},
			{"UTF8 bytes", strings.Repeat("界", 21846)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				d, c, i := boardFixture(t)
				sink := sinkFor(t)
				_, _, err := d.DismissHeraBoardItem(c.ID, i.ID, i.UpdatedAt, tc.note)
				testutil.ErrorIs(t, err, ErrHeraBoardInvalid)
				testutil.Equal(t, len(sink.board()), 0)
			})
		}
	})
}

func TestBoardEventsOnSharedPaths(t *testing.T) {
	d, c, _ := boardFixture(t)
	sink := sinkFor(t)
	i, err := d.PostHeraBoardItem(c.ID, "fyi", "Hi", "b")
	testutil.NoError(t, err)
	_, err = d.UpdateHeraBoardItem(c.ID, i.ID, "fyi", "Hi", "b")
	testutil.NoError(t, err)
	_, err = d.UpdateHeraBoardItem(c.ID, i.ID, "fyi", "Hi", "b2")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ReadHeraBoardItemAt(c.ID, i.ID, time.Now().Add(-time.Hour)))
	i2, err := d.UpdateHeraBoardItem(c.ID, i.ID, "fyi", "Hi", "b2")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ReadHeraBoardItemAt(c.ID, i.ID, i2.UpdatedAt))
	testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "agent", ""))
	var ops []string
	for _, e := range sink.board() {
		for _, op := range []string{"posted", "updated", "read_state", "resolved"} {
			if strings.Contains(string(e.Payload), `"operation":"`+op+`"`) {
				ops = append(ops, op)
			}
		}
	}
	testutil.DeepEqual(t, ops, []string{"posted", "updated", "read_state", "resolved"})
}

func TestHeraMessagesPageForTask(t *testing.T) {
	d := heraTestDB(t)
	o := mkOrch(t, d, "orch")
	coord := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	w := mkRole(t, d, o.ID, "w", HeraKindWorker)
	_, err := d.CreateHeraBinding(CreateHeraBindingInput{RoleID: w.ID, ArgusTaskID: "t1", WorktreePath: "/wt"})
	testutil.NoError(t, err)

	t.Run("empty", func(t *testing.T) {
		msgs, more, err := d.HeraMessagesPageForTask("t1", 10, 0)
		testutil.NoError(t, err)
		testutil.Equal(t, len(msgs), 0)
		testutil.Equal(t, more, false)
	})

	var ids []int64
	for i := 0; i < 5; i++ {
		ids = append(ids, mkMsg(t, d, coord.ID, w.ID, "b", "t", nil).ID)
	}
	_, err = d.conn.Exec(`UPDATE hera_messages SET sent_at='2026-01-01T00:00:00Z'`)
	testutil.NoError(t, err)
	before := snapshotMsgState(t, d)

	t.Run("pages are stable by id despite identical timestamps", func(t *testing.T) {
		p1, more, err := d.HeraMessagesPageForTask("t1", 2, 0)
		testutil.NoError(t, err)
		testutil.Equal(t, more, true)
		testutil.DeepEqual(t, []int64{p1[0].ID, p1[1].ID}, ids[3:5])
		p2, more, err := d.HeraMessagesPageForTask("t1", 2, p1[0].ID)
		testutil.NoError(t, err)
		testutil.Equal(t, more, true)
		testutil.DeepEqual(t, []int64{p2[0].ID, p2[1].ID}, ids[1:3])
		p3, more, err := d.HeraMessagesPageForTask("t1", 2, p2[0].ID)
		testutil.NoError(t, err)
		testutil.Equal(t, more, false)
		testutil.DeepEqual(t, []int64{p3[0].ID}, ids[0:1])
	})
	t.Run("exact fit has no more", func(t *testing.T) {
		_, more, err := d.HeraMessagesPageForTask("t1", 5, 0)
		testutil.NoError(t, err)
		testutil.Equal(t, more, false)
	})
	t.Run("never mutates read or delivery state", func(t *testing.T) {
		testutil.Equal(t, snapshotMsgState(t, d), before)
		n, _ := d.HeraInbox(w.ID)
		testutil.Equal(t, len(n), 5)
	})
}

func snapshotMsgState(t *testing.T, d *DB) string {
	t.Helper()
	rows, err := d.conn.Query(`SELECT id, COALESCE(read_at,''), delivery_mode, COALESCE(delivered_at,'') FROM hera_messages ORDER BY id`)
	testutil.NoError(t, err)
	defer rows.Close()
	out := ""
	for rows.Next() {
		var id int64
		var a, b, c string
		testutil.NoError(t, rows.Scan(&id, &a, &b, &c))
		out += a + "|" + b + "|" + c + ";"
	}
	return out
}

func TestBoardLegacyReadEmitsOnce(t *testing.T) {
	d, c, i := boardFixture(t)
	sink := sinkFor(t)
	testutil.NoError(t, d.ReadHeraBoardItemAt(c.ID, i.ID, i.UpdatedAt))
	testutil.NoError(t, d.ReadHeraBoardItemAt(c.ID, i.ID, i.UpdatedAt))
	testutil.NoError(t, d.ReadHeraBoardItem(c.ID, i.ID))
	testutil.Equal(t, len(sink.board()), 1)
	testutil.ErrorIs(t, d.ReadHeraBoardItem(c.ID, i.ID+1), ErrHeraBoardNotFound)
}
