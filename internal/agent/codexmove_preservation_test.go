package agent

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCodexMoveFailedIndexPreservesRollout(t *testing.T) {
	for _, existed := range []bool{false, true} {
		name := "new"
		if existed {
			name = "existing"
		}
		for _, timing := range []string{"BEFORE", "AFTER"} {
			t.Run(name+timing, func(t *testing.T) {
				f := newCodexMoveFixture(t)
				dstDB := filepath.Join(f.dstHome, codexStateDB)
				dst := filepath.Join(f.dstHome, "sessions", "2026", "11", "07", filepath.Base(f.rollout))
				if existed {
					_, _, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
					testutil.NoError(t, err)
					testutil.NoError(t, f.d.SetTaskAccount(f.task.ID, "", false))
					testutil.NoError(t, os.WriteFile(dst, []byte("independent target turns"), 0600))
					execMoveSQL(t, dstDB, `UPDATE threads SET tokens_used=999`)
				}
				event := "INSERT"
				if existed && timing == "AFTER" {
					event = "UPDATE"
				}
				execMoveSQL(t, dstDB, `CREATE TRIGGER refuse_write `+timing+` `+event+` ON threads BEGIN SELECT RAISE(FAIL, 'write refused'); END`)
				srcBefore, err := os.ReadFile(f.rollout)
				testutil.NoError(t, err)
				_, _, err = MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
				testutil.Error(t, err)
				body, err := os.ReadFile(dst)
				if existed {
					testutil.NoError(t, err)
					testutil.Equal(t, string(body), "independent target turns")
				} else {
					testutil.True(t, os.IsNotExist(err))
				}
				_, _, tokens, found := codexThreadRow(t, dstDB)
				testutil.Equal(t, found, existed)
				if existed {
					testutil.Equal(t, tokens, 999)
				}
				after, err := os.ReadFile(f.rollout)
				testutil.NoError(t, err)
				testutil.DeepEqual(t, after, srcBefore)
				got, err := f.d.Get(f.task.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, got.Account, "")
			})
		}
	}
}

func fullMoveRow(t *testing.T, path string) []any {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	defer func() { _ = conn.Close() }()
	rows, err := conn.Query(`SELECT * FROM threads WHERE id=?`, codexMoveSID)
	testutil.NoError(t, err)
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	testutil.NoError(t, err)
	testutil.True(t, rows.Next())
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	testutil.NoError(t, rows.Scan(ptrs...))
	return values
}

func TestCodexMovePreservesCredentialsAndMetadata(t *testing.T) {
	for _, mode := range []string{"success", "failure", "move back"} {
		t.Run(mode, func(t *testing.T) {
			f := newCodexMoveFixture(t)
			srcDB, dstDB := filepath.Join(f.srcHome, codexStateDB), filepath.Join(f.dstHome, codexStateDB)
			for _, path := range []string{srcDB, dstDB} {
				execMoveSQL(t, path, `ALTER TABLE threads ADD COLUMN folder_id TEXT`)
				execMoveSQL(t, path, `ALTER TABLE threads ADD COLUMN project_id TEXT`)
			}
			execMoveSQL(t, srcDB, `UPDATE threads SET folder_id='source-folder',project_id='source-project'`)
			_, _, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
			testutil.NoError(t, err)
			testutil.NoError(t, f.d.SetTaskAccount(f.task.ID, "", false))
			execMoveSQL(t, dstDB, `UPDATE threads SET creator_user_id='target-user',extra_local='target-local',folder_id='target-folder',project_id='target-project'`)
			if mode == "failure" {
				execMoveSQL(t, dstDB, `CREATE TRIGGER refuse_insert BEFORE INSERT ON threads BEGIN SELECT RAISE(FAIL, 'write refused'); END`)
			}
			for home, secret := range map[string]string{f.srcHome: "source secret", f.dstHome: "target secret"} {
				testutil.NoError(t, os.WriteFile(filepath.Join(home, "auth.json"), []byte(secret), 0600))
			}
			before := fullMoveRow(t, srcDB)
			rolloutBefore, err := os.ReadFile(f.rollout)
			testutil.NoError(t, err)
			_, _, err = MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
			if mode == "failure" {
				testutil.Error(t, err)
			} else {
				testutil.NoError(t, err)
			}
			testutil.DeepEqual(t, fullMoveRow(t, srcDB), before)
			after, err := os.ReadFile(f.rollout)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, after, rolloutBefore)
			conn, err := sql.Open("sqlite", dstDB)
			testutil.NoError(t, err)
			var creator, local, folder, project string
			testutil.NoError(t, conn.QueryRow(`SELECT creator_user_id,extra_local,folder_id,project_id FROM threads WHERE id=?`, codexMoveSID).Scan(&creator, &local, &folder, &project))
			testutil.NoError(t, conn.Close())
			testutil.Equal(t, creator, "target-user")
			testutil.Equal(t, local, "target-local")
			testutil.Equal(t, folder, "target-folder")
			testutil.Equal(t, project, "target-project")
			if mode == "move back" {
				backRow := fullMoveRow(t, dstDB)
				backPath, _, _, found := codexThreadRow(t, dstDB)
				testutil.True(t, found)
				backBytes, err := os.ReadFile(backPath)
				testutil.NoError(t, err)
				_, _, err = MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "default"})
				testutil.NoError(t, err)
				testutil.DeepEqual(t, fullMoveRow(t, srcDB), before)
				testutil.DeepEqual(t, fullMoveRow(t, dstDB), backRow)
				backAfter, err := os.ReadFile(backPath)
				testutil.NoError(t, err)
				testutil.DeepEqual(t, backAfter, backBytes)
			}
			for home, secret := range map[string]string{f.srcHome: "source secret", f.dstHome: "target secret"} {
				body, err := os.ReadFile(filepath.Join(home, "auth.json"))
				testutil.NoError(t, err)
				testutil.Equal(t, string(body), secret)
			}
		})
	}
}
