package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

func seedRenameProject(t *testing.T, d *DB) {
	t.Helper()
	testutil.NoError(t, d.SetProject("old", config.Project{Path: "/repo"}))
	testutil.NoError(t, d.SetProjectAccount("old", "acct"))
	for _, q := range []string{
		`INSERT INTO tasks (id, name, project, created_at) VALUES ('t1','n','old','x')`,
		`INSERT INTO scheduled_tasks (id,name,project,prompt,created_at) VALUES ('s1','n','old','p','x')`,
		`INSERT INTO kb_pending_tasks (name,project,source_file,created_at) VALUES ('n','old','f','x')`,
		`INSERT INTO sandbox_proposals (id,task_id,project,entries,created_at) VALUES ('p1','t1','old','[]','x')`,
		`INSERT INTO hera_orchestrators (id,name,created_at) VALUES (1,'o','x')`,
		`INSERT INTO hera_roles (id,orchestrator_id,name,kind,argus_project,created_at) VALUES (1,1,'r','worker','old','x')`,
	} {
		if _, err := d.conn.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
}

func countWhere(t *testing.T, d *DB, table, col, val string) int {
	t.Helper()
	var n int
	testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE `+col+` = ?`, val).Scan(&n))
	return n
}

func TestRenameProject(t *testing.T) {
	refs := [][2]string{{"projects", "name"}, {"tasks", "project"}, {"scheduled_tasks", "project"}, {"kb_pending_tasks", "project"}, {"sandbox_proposals", "project"}, {"hera_roles", "argus_project"}}

	t.Run("rewrites all references", func(t *testing.T) {
		d := testDB(t)
		seedRenameProject(t, d)
		testutil.NoError(t, d.RenameProject("old", " new "))
		for _, r := range refs {
			testutil.Equal(t, countWhere(t, d, r[0], r[1], "old"), 0)
			testutil.Equal(t, countWhere(t, d, r[0], r[1], "new"), 1)
		}
		var raw string
		testutil.NoError(t, d.conn.QueryRow(`SELECT value FROM config WHERE key = ?`, ConfigKeyProjectAccounts).Scan(&raw))
		testutil.Equal(t, raw, `{"new":"acct"}`)
	})

	t.Run("errors", func(t *testing.T) {
		d := testDB(t)
		seedRenameProject(t, d)
		testutil.NoError(t, d.SetProject("taken", config.Project{Path: "/x"}))
		testutil.ErrorIs(t, d.RenameProject("missing", "z"), ErrProjectNotFound)
		testutil.ErrorIs(t, d.RenameProject("old", "taken"), ErrProjectExists)
		for _, bad := range []string{"", "  ", "a/b", "old", ".", "..", `a\b`, "a\nb"} {
			testutil.ErrorIs(t, d.RenameProject("old", bad), ErrInvalidProjectName)
		}
		testutil.Equal(t, countWhere(t, d, "tasks", "project", "old"), 1)
	})

	t.Run("rollback on failure", func(t *testing.T) {
		d := testDB(t)
		seedRenameProject(t, d)
		_, err := d.conn.Exec(`CREATE TRIGGER fail_hera BEFORE UPDATE ON hera_roles BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		testutil.NoError(t, err)
		if err := d.RenameProject("old", "new"); err == nil {
			t.Fatal("expected failure")
		}
		for _, r := range refs {
			testutil.Equal(t, countWhere(t, d, r[0], r[1], "old"), 1)
			testutil.Equal(t, countWhere(t, d, r[0], r[1], "new"), 0)
		}
	})
}
