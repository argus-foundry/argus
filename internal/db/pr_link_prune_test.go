package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestManualPR_PruneRollback(t *testing.T) {
	for _, mode := range []string{"completed", "selected"} {
		t.Run(mode, func(t *testing.T) {
			d := testDB(t)
			testutil.NoError(t, d.Add(&model.Task{ID: "linked", Name: "n", Status: model.StatusComplete}))
			testutil.NoError(t, d.SetManualPR("linked", "https://github.com/o/r/pull/9"))
			_, err := d.conn.Exec(`CREATE TRIGGER fail_prune BEFORE DELETE ON tasks BEGIN SELECT RAISE(FAIL, 'blocked'); END`)
			testutil.NoError(t, err)
			if mode == "completed" {
				_, _, err = d.PruneCompleted()
			} else {
				_, _, err = d.PruneTasks([]string{"linked"})
			}
			testutil.Equal(t, err != nil, true)
			link, err := d.ManualPR("linked")
			testutil.NoError(t, err)
			testutil.Equal(t, link, "https://github.com/o/r/pull/9")
			task, err := d.Get("linked")
			testutil.NoError(t, err)
			testutil.Equal(t, task.ID, "linked")
		})
	}
}
