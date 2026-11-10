package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSaveProject(t *testing.T) {
	for _, enabled := range []string{"inherit", "on", "off"} {
		t.Run(enabled, func(t *testing.T) {
			d := testDB(t)
			p := config.Project{Path: "/repo", Branch: "main", Backend: "codex", Profile: "lean", Sandbox: config.ProjectSandboxConfig{DenyRead: []string{"secret"}, ExtraWrite: []string{"cache"}, AllowAppleEvents: []string{"Finder"}}}
			if enabled != "inherit" {
				v := enabled == "on"
				p.Sandbox.Enabled = &v
			}
			got, changed, err := d.SaveProject("p", p, ProjectSaveOptions{})
			testutil.NoError(t, err)
			testutil.Equal(t, changed, true)
			testutil.DeepEqual(t, got, p)
			got, changed, err = d.SaveProject("p", p, ProjectSaveOptions{})
			testutil.NoError(t, err)
			testutil.Equal(t, changed, false)
			testutil.DeepEqual(t, got, p)
			newer := p
			newer.Profile = "default"
			testutil.NoError(t, d.SetProject("p", newer))
			stale := p
			stale.Path = "/renamed"
			stale.Profile = ""
			got, changed, err = d.SaveProject("p", stale, ProjectSaveOptions{PreserveProfile: true, RequireExisting: true})
			testutil.NoError(t, err)
			testutil.Equal(t, changed, true)
			testutil.Equal(t, got.Profile, "default")
			rows, err := d.Projects()
			testutil.NoError(t, err)
			testutil.DeepEqual(t, rows["p"], got)
			got, changed, err = d.SaveProject("p", stale, ProjectSaveOptions{RequireExisting: true})
			testutil.NoError(t, err)
			testutil.Equal(t, changed, true)
			testutil.Equal(t, got.Profile, "")
		})
	}
}

func TestSaveProject_FailureLeavesRowUnchanged(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		d := testDB(t)
		_, changed, err := d.SaveProject("missing", config.Project{Path: "/repo"}, ProjectSaveOptions{RequireExisting: true})
		testutil.ErrorIs(t, err, ErrProjectNotFound)
		testutil.Equal(t, changed, false)
		rows, err := d.Projects()
		testutil.NoError(t, err)
		testutil.Equal(t, len(rows), 0)
	})
	t.Run("closed", func(t *testing.T) {
		d := testDB(t)
		testutil.NoError(t, d.Close())
		_, changed, err := d.SaveProject("p", config.Project{}, ProjectSaveOptions{})
		testutil.NotNil(t, err)
		testutil.Equal(t, changed, false)
	})
	t.Run("read fails", func(t *testing.T) {
		d := testDB(t)
		_, err := d.conn.Exec(`DROP TABLE projects`)
		testutil.NoError(t, err)
		_, changed, err := d.SaveProject("p", config.Project{}, ProjectSaveOptions{})
		testutil.NotNil(t, err)
		testutil.Equal(t, changed, false)
	})
	t.Run("write fails", func(t *testing.T) {
		d := testDB(t)
		p := config.Project{Path: "/repo", Profile: "lean"}
		testutil.NoError(t, d.SetProject("p", p))
		_, err := d.conn.Exec(`CREATE TRIGGER reject_project BEFORE INSERT ON projects BEGIN SELECT RAISE(ABORT,'rejected'); END`)
		testutil.NoError(t, err)
		_, changed, err := d.SaveProject("p", config.Project{Path: "/new"}, ProjectSaveOptions{PreserveProfile: true})
		testutil.NotNil(t, err)
		testutil.Equal(t, changed, false)
		rows, err := d.Projects()
		testutil.NoError(t, err)
		testutil.DeepEqual(t, rows["p"], p)
	})
}
