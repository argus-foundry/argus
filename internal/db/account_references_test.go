package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskAccountReferences(t *testing.T) {
	d := testDB(t)
	task := &model.Task{ID: "a", Name: "archived", Account: "work", Archived: true}
	testutil.NoError(t, d.Add(task))
	testutil.NoError(t, d.Add(&model.Task{ID: "b", Name: "other", Account: "personal"}))
	resident, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	resident.Account = "work"
	testutil.NoError(t, d.Update(resident))
	// Unrelated task columns cannot hide an account reference.
	_, err = d.conn.Exec(`UPDATE tasks SET agent_pid='unreadable' WHERE id='a'`)
	testutil.NoError(t, err)
	ids, err := d.TaskAccountReferences("work")
	testutil.NoError(t, err)
	testutil.DeepEqual(t, ids, []string{resident.ID, "a"})
	ids, err = d.TaskAccountReferences("missing")
	testutil.NoError(t, err)
	testutil.Equal(t, len(ids), 0)
	_, err = d.conn.Exec(`ALTER TABLE tasks RENAME TO stored_tasks`)
	testutil.NoError(t, err)
	_, err = d.conn.Exec(`CREATE VIEW tasks AS SELECT id, NULL AS account FROM stored_tasks`)
	testutil.NoError(t, err)
	_, err = d.TaskAccountReferences("work")
	testutil.True(t, err != nil)
	testutil.NoError(t, d.Close())
	_, err = d.TaskAccountReferences("work")
	testutil.True(t, err != nil)
}
