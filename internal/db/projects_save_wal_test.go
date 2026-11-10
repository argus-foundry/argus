package db

import (
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSaveProject_IndependentWALWriter(t *testing.T) {
	for _, sameProject := range []bool{true, false} {
		name := "same project"
		if !sameProject {
			name = "unrelated project"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			path := filepath.Join(t.TempDir(), "data.sql")
			first, err := Open(path)
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = first.Close() })
			second, err := Open(path)
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = second.Close() })
			second.conn.SetMaxOpenConns(1)
			_, err = second.conn.Exec(`PRAGMA busy_timeout=0`)
			testutil.NoError(t, err)
			testutil.NoError(t, first.SetProject("p", config.Project{Path: "/repo", Profile: "lean"}))
			testutil.NoError(t, first.SetProject("other", config.Project{Path: "/other", Profile: "lean"}))
			var journal string
			testutil.NoError(t, first.conn.QueryRow(`PRAGMA journal_mode`).Scan(&journal))
			testutil.Equal(t, journal, "wal")
			key := "other"
			if sameProject {
				key = "p"
			}
			write := func() error {
				_, err := second.conn.Exec(`UPDATE projects SET profile='default' WHERE name=?`, key)
				return err
			}
			var externalErr error
			readReached := false
			options := ProjectSaveOptions{PreserveProfile: true, RequireExisting: true, afterRead: func() {
				readReached = true
				externalErr = write()
			}}
			got, changed, saveErr := first.SaveProject("p", config.Project{Path: "/renamed"}, options)
			testutil.Equal(t, readReached, true)
			testutil.NoError(t, saveErr)
			testutil.Equal(t, changed, true)
			testutil.Equal(t, got.Path, "/renamed")
			testutil.Equal(t, got.Profile, "lean")
			testutil.NotNil(t, externalErr)
			if externalErr != nil {
				code, ok := externalErr.(interface{ Code() int })
				testutil.True(t, ok)
				if ok {
					testutil.Equal(t, code.Code(), 5)
				}
				testutil.NoError(t, write())
			}
			rows, err := second.Projects()
			testutil.NoError(t, err)
			testutil.Equal(t, rows["p"].Path, "/renamed")
			expected := "lean"
			if sameProject {
				expected = "default"
			}
			testutil.Equal(t, rows["p"].Profile, expected)
			testutil.Equal(t, rows[key].Profile, "default")
		})
	}
}

func TestSaveProject_ReleasesWALWriteLock(t *testing.T) {
	for _, name := range []string{"no-op", "missing", "rejected", "commit rejected"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			path := filepath.Join(t.TempDir(), "data.sql")
			first, err := Open(path)
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = first.Close() })
			second, err := Open(path)
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = second.Close() })
			second.conn.SetMaxOpenConns(1)
			_, err = second.conn.Exec(`PRAGMA busy_timeout=0`)
			testutil.NoError(t, err)
			original := config.Project{Path: "/repo", Profile: "lean"}
			testutil.NoError(t, first.SetProject("p", original))
			key := "p"
			next := original
			if name == "missing" {
				key = "missing"
			}
			if name == "rejected" || name == "commit rejected" {
				trigger := `CREATE TRIGGER reject_project BEFORE INSERT ON projects BEGIN SELECT RAISE(ABORT,'rejected'); END`
				if name == "commit rejected" {
					_, err = first.conn.Exec(`CREATE TABLE save_parent(id TEXT PRIMARY KEY); CREATE TABLE save_child(parent TEXT REFERENCES save_parent(id) DEFERRABLE INITIALLY DEFERRED)`)
					testutil.NoError(t, err)
					trigger = `CREATE TRIGGER reject_project AFTER INSERT ON projects BEGIN INSERT INTO save_child(parent) VALUES ('missing'); END`
				}
				_, err = first.conn.Exec(trigger)
				testutil.NoError(t, err)
				next.Path = "/rejected"
			}
			_, changed, err := first.SaveProject(key, next, ProjectSaveOptions{RequireExisting: true})
			testutil.Equal(t, changed, false)
			if name == "no-op" {
				testutil.NoError(t, err)
			} else {
				testutil.NotNil(t, err)
			}
			if name == "rejected" || name == "commit rejected" {
				_, err = first.conn.Exec(`DROP TRIGGER reject_project`)
				testutil.NoError(t, err)
			}
			_, err = second.conn.Exec(`UPDATE projects SET profile='default' WHERE name='p'`)
			testutil.NoError(t, err)
			rows, err := second.Projects()
			testutil.NoError(t, err)
			testutil.Equal(t, rows["p"].Profile, "default")
			testutil.Equal(t, rows["p"].Path, "/repo")
		})
	}
}
