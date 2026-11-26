package hera

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestArchiveVerdictRefusesBoardWork(t *testing.T) {
	for _, kind := range []string{"action", "todo"} {
		t.Run(kind, func(t *testing.T) {
			d := archDB(t)
			_, sub, cc, _ := subCoord(t, d)
			pending, blocking, trigger := true, false, "after release"
			p := db.HeraBoardMetadataPatch{Trigger: &trigger, TriggerPending: &pending}
			if kind == "action" {
				p.Blocking = &blocking
			}
			_, err := d.PostHeraBoardItemWithMetadata(cc.ID, kind, "later", "body", p)
			testutil.NoError(t, err)
			v := ArchivableVerdict(d, gitOK, sub)
			testutil.Equal(t, v.Archivable, false)
			testutil.Contains(t, v.Reason, kind)
		})
	}
}
