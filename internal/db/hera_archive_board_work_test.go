package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestArchiveRefusesBoardWorkInTransaction(t *testing.T) {
	for _, kind := range []string{"action", "todo"} {
		t.Run(kind, func(t *testing.T) {
			f := newUnitFixture(t)
			p := HeraBoardMetadataPatch{Trigger: boardText("later")}
			if kind == "action" {
				p.Blocking = boardBool(false)
			}
			i, err := f.d.PostHeraBoardItemWithMetadata(f.cc.ID, kind, "unsettled", "body", p)
			testutil.NoError(t, err)
			_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
			testutil.Error(t, err)
			testutil.Contains(t, err.Error(), kind)
			current, err := f.d.Get(f.task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, current.Archived, false)
			testutil.NoError(t, f.d.ResolveHeraBoardItem(f.cc.ID, i.ID, "agent", "done"))
			// Retire the completion notice so existing unread-outbox policy is
			// satisfied before verifying the now-settled archive can proceed.
			_, err = f.d.HeraInbox(f.cc.ID)
			testutil.NoError(t, err)
			_, err = f.d.ArchiveHeraUnit(ArchiveUnitInput{RoleID: f.sub.ID})
			testutil.NoError(t, err)
		})
	}
}
