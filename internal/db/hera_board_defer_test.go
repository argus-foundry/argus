package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardDeferredPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	d, err := Open(path)
	testutil.NoError(t, err)
	o := mkOrch(t, d, "orch")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	i, err := d.PostHeraBoardItem(c.ID, "fyi", "Later", "body")
	testutil.NoError(t, err)
	got, _, err := d.SetHeraBoardItemDeferred(c.ID, i.ID, true, i.UpdatedAt)
	testutil.NoError(t, err)
	testutil.NoError(t, d.Close())
	d, err = Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	items, err := d.ListHeraBoardItems(c.ID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].DeferredAt.Equal(*got.DeferredAt), true)
	counts, err := d.HeraBoardOpenCounts()
	testutil.NoError(t, err)
	testutil.Equal(t, counts[c.ID].FYIs, 0)
	testutil.Equal(t, counts[c.ID].Deferred, 1)
	testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "human", ""))
	counts, err = d.HeraBoardOpenCounts()
	testutil.NoError(t, err)
	testutil.Equal(t, counts[c.ID].Deferred, 0)
}

func TestBoardDeferredResidentApproval(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	i, n, err := d.SetHeraBoardItemDeferred(a.RoleID, a.BoardID, true, a.CreatedAt)
	testutil.NoError(t, err)
	testutil.Equal(t, n, 0)
	testutil.Equal(t, i.ResidentApproval, true)
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, false, now))
}

func TestBoardDeferral(t *testing.T) {
	for _, kind := range []string{"decision", "fyi", "action", "todo"} {
		t.Run(kind, func(t *testing.T) {
			d, c, _ := boardFixture(t)
			var patch HeraBoardMetadataPatch
			if kind == "action" {
				blocking := true
				patch.Blocking = &blocking
			}
			i, err := d.PostHeraBoardItemWithMetadata(c.ID, kind, "Later", "body", patch)
			testutil.NoError(t, err)
			sink := sinkFor(t)
			got, n, err := d.SetHeraBoardItemDeferred(c.ID, i.ID, true, i.UpdatedAt)
			testutil.NoError(t, err)
			testutil.Equal(t, got.DeferredAt.IsZero(), false)
			testutil.Equal(t, got.Read, false)
			testutil.Equal(t, got.UpdatedAt.Equal(i.UpdatedAt), true)
			testutil.Equal(t, got.AttentionUnread(), false)
			testutil.Equal(t, n, 1) // fixture decision still rings
			testutil.Contains(t, string(sink.board()[0].Payload), `"operation":"deferred"`)
			again, _, err := d.SetHeraBoardItemDeferred(c.ID, i.ID, true, i.UpdatedAt)
			testutil.NoError(t, err)
			testutil.Equal(t, again.DeferredAt.Equal(*got.DeferredAt), true)
			testutil.Equal(t, len(sink.board()), 1)
			_, _, err = d.SetHeraBoardItemRead(c.ID, i.ID, true, i.UpdatedAt)
			testutil.NoError(t, err)
			got, err = d.UpdateHeraBoardItem(c.ID, i.ID, kind, "Later", "changed")
			testutil.NoError(t, err)
			testutil.Equal(t, got.Read, false)
			testutil.Equal(t, got.DeferredAt.Equal(*again.DeferredAt), true)
			_, _, err = d.SetHeraBoardItemDeferred(c.ID, i.ID, false, i.UpdatedAt)
			testutil.ErrorIs(t, err, ErrHeraBoardConflict)
			got, _, err = d.SetHeraBoardItemDeferred(c.ID, i.ID, false, got.UpdatedAt)
			testutil.NoError(t, err)
			testutil.Nil(t, got.DeferredAt)
			testutil.Equal(t, got.Read, false)
			_, _, err = d.SetHeraBoardItemDeferred(c.ID, i.ID, true, got.UpdatedAt)
			testutil.NoError(t, err)
			testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "agent", "done"))
			_, _, err = d.SetHeraBoardItemDeferred(c.ID, i.ID, false, got.UpdatedAt)
			testutil.ErrorIs(t, err, ErrHeraBoardResolved)
		})
	}
	t.Run("invalid and missing", func(t *testing.T) {
		d, c, i := boardFixture(t)
		_, _, err := d.SetHeraBoardItemDeferred(c.ID, i.ID, true, time.Time{})
		testutil.ErrorIs(t, err, ErrHeraBoardInvalid)
		_, _, err = d.SetHeraBoardItemDeferred(c.ID, 999, true, i.UpdatedAt)
		testutil.ErrorIs(t, err, ErrHeraBoardNotFound)
		_, _, err = d.SetHeraBoardItemDeferred(c.ID+1, i.ID, true, i.UpdatedAt)
		testutil.ErrorIs(t, err, ErrHeraBoardNotFound)
	})
}
