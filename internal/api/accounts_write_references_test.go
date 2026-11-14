package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestAccountWrite_DeleteSoleReference(t *testing.T) {
	for _, kind := range []string{"task", "archived_task", "global_default", "project_default", "tier", "artifact_home", "resident_task", "resident_config"} {
		t.Run(kind, func(t *testing.T) {
			toml := ""
			switch kind {
			case "tier":
				toml = "[backend_routing]\n[[backend_routing.tier]]\nbackend = \"claude\"\naccount = \"work\"\n"
			case "artifact_home":
				toml = "[project_artifact_accounts]\np = \"work\"\n"
			case "resident_config":
				toml = "[resident]\naccount = \"work\"\n"
			}
			srv, d := accountsServer(t, toml)
			original := config.Account{Label: "Work", ClaudeConfigDir: filepath.Join(t.TempDir(), "login")}
			testutil.NoError(t, d.SetAccount("work", original))
			want := map[string]any{}
			switch kind {
			case "task", "archived_task":
				task := &model.Task{Name: "bound", Account: "work", Archived: kind == "archived_task"}
				testutil.NoError(t, d.Add(task))
				want["tasks"] = []any{task.ID}
			case "global_default":
				testutil.NoError(t, d.SetDefaultAccount("work"))
				want["default"] = true
			case "project_default":
				testutil.NoError(t, d.SetProjectAccount("p", "work"))
				want["projects"] = []any{"p"}
			case "tier":
				want["tiers"] = []any{"claude"}
			case "artifact_home":
				want["artifact_projects"] = []any{"p"}
				testutil.Equal(t, d.Config().ArtifactAccount("p"), "work")
			case "resident_task":
				resident, _, err := d.EnsureResidentTask(t.TempDir())
				testutil.NoError(t, err)
				resident.Account = "work"
				testutil.NoError(t, d.Update(resident))
				want["tasks"] = []any{resident.ID}
			case "resident_config":
				want["resident"] = true
			}
			before := d.Config()
			rs := captureEvents(t)
			w := serveAccountRequest(srv, acctReq("DELETE", "/api/accounts/work", "", "master"))
			testutil.Equal(t, w.Code, http.StatusConflict)
			var body struct {
				References map[string]any `json:"references"`
			}
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			testutil.DeepEqual(t, body.References, want)
			rows, err := d.Accounts()
			testutil.NoError(t, err)
			testutil.DeepEqual(t, rows["work"], original)
			after := d.Config()
			testutil.Equal(t, after.DefaultAccount, before.DefaultAccount)
			testutil.DeepEqual(t, after.ProjectAccounts, before.ProjectAccounts)
			testutil.DeepEqual(t, after.ProjectArtifactAccounts, before.ProjectArtifactAccounts)
			testutil.Equal(t, after.ArtifactAccount("p"), before.ArtifactAccount("p"))
			testutil.DeepEqual(t, after.Resident.Accounts, before.Resident.Accounts)
			testutil.DeepEqual(t, after.BackendRouting, before.BackendRouting)
			testutil.Equal(t, len(rs.configChanged()), 0)
		})
	}
}

func TestAccountWrite_DeleteReferenceReadFailure(t *testing.T) {
	for _, kind := range []string{"unreadable_account", "query_failure", "unreadable_unrelated_column"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data.sql")
			d, err := db.Open(path)
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			srv := New(d, agent.NewRunner(nil), "test-token", nil, nil)
			testutil.NoError(t, d.SetAccount("work", config.Account{ClaudeConfigDir: filepath.Join(t.TempDir(), "login")}))
			task := &model.Task{Name: "bound", Account: "work"}
			testutil.NoError(t, d.Add(task))
			raw, err := sql.Open("sqlite", path)
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = raw.Close() })
			switch kind {
			case "unreadable_account":
				_, err = raw.Exec(`ALTER TABLE tasks RENAME TO stored_tasks`)
				testutil.NoError(t, err)
				_, err = raw.Exec(`CREATE VIEW tasks AS SELECT id, NULL AS account FROM stored_tasks`)
				testutil.NoError(t, err)
			case "query_failure":
				_, err = raw.Exec(`ALTER TABLE tasks RENAME TO stored_tasks`)
				testutil.NoError(t, err)
			case "unreadable_unrelated_column":
				_, err = raw.Exec(`UPDATE tasks SET agent_pid='unscannable'`)
				testutil.NoError(t, err)
			}
			rs := captureEvents(t)
			w := serveAccountRequest(srv, acctReq("DELETE", "/api/accounts/work", "", "master"))
			want := http.StatusInternalServerError
			if kind == "unreadable_unrelated_column" {
				want = http.StatusConflict
			}
			testutil.Equal(t, w.Code, want)
			rows, err := d.Accounts()
			testutil.NoError(t, err)
			_, present := rows["work"]
			testutil.True(t, present)
			testutil.Equal(t, len(rs.configChanged()), 0)
		})
	}
}
