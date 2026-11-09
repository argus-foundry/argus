package db

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardStrictZeroRevision(t *testing.T) {
	for _, operation := range []string{"read", "unread", "dismiss"} {
		t.Run(operation, func(t *testing.T) {
			d, c, item := boardFixture(t)
			edited, err := d.UpdateHeraBoardItem(c.ID, item.ID, "decision", "Edited", "unseen content")
			testutil.NoError(t, err)
			sink := sinkFor(t)
			var got *HeraBoardItem
			var unread int
			if operation == "dismiss" {
				got, unread, err = d.DismissHeraBoardItem(c.ID, item.ID, time.Time{}, "stop")
			} else {
				got, unread, err = d.SetHeraBoardItemRead(c.ID, item.ID, operation == "read", time.Time{})
			}
			testutil.ErrorIs(t, err, ErrHeraBoardInvalid)
			testutil.Nil(t, got)
			testutil.Equal(t, unread, 0)
			testutil.Equal(t, len(sink.board()), 0)
			items, err := d.ListHeraBoardItems(c.ID, true)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, items[0], edited)
			notices, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(notices), 0)
		})
	}
}

func TestBoardLegacyResolveWithoutExpectedRevision(t *testing.T) {
	d, c, item := boardFixture(t)
	_, err := d.UpdateHeraBoardItem(c.ID, item.ID, "decision", "Edited", "new content")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, item.ID, "human", "stop"))
	items, err := d.ListHeraBoardItems(c.ID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, items[0].Resolved, true)
	testutil.Equal(t, items[0].Body, "new content")
	notices, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(notices), 1)
}
