package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCodexMoveRejectsSourceSessionsAlias(t *testing.T) {
	for _, target := range []string{".", "sibling", "outside"} {
		t.Run(target, func(t *testing.T) {
			f := newCodexMoveFixture(t)
			sessions := filepath.Join(f.srcHome, "sessions")
			testutil.NoError(t, os.Rename(sessions, filepath.Join(f.srcHome, "saved-sessions")))
			alias := target
			switch target {
			case "outside":
				alias = t.TempDir()
			case "sibling":
				alias = "private"
				testutil.NoError(t, os.Mkdir(filepath.Join(f.srcHome, alias), 0700))
			}
			credentialPath := filepath.Join(f.srcHome, "auth.json")
			if target != "." {
				credentialPath = filepath.Join(alias, "auth.json")
				if target == "sibling" {
					credentialPath = filepath.Join(f.srcHome, alias, "auth.json")
				}
				testutil.NoError(t, os.WriteFile(credentialPath, []byte("credential sentinel"), 0600))
			}
			testutil.NoError(t, os.Symlink(alias, sessions))
			srcDB, dstDB := filepath.Join(f.srcHome, codexStateDB), filepath.Join(f.dstHome, codexStateDB)
			execMoveSQL(t, srcDB, `UPDATE threads SET rollout_path=?`, filepath.Join(sessions, "auth.json"))
			before := fullMoveRow(t, srcDB)
			auth, err := os.ReadFile(credentialPath)
			testutil.NoError(t, err)
			_, _, err = MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
			testutil.ErrorIs(t, err, ErrAccount)
			got, err := f.d.Get(f.task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Account, "")
			testutil.Equal(t, got.SessionID, codexMoveSID)
			testutil.DeepEqual(t, fullMoveRow(t, srcDB), before)
			_, _, _, found := codexThreadRow(t, dstDB)
			testutil.False(t, found)
			_, err = os.Stat(filepath.Join(f.dstHome, "sessions", "auth.json"))
			testutil.True(t, os.IsNotExist(err))
			after, err := os.ReadFile(credentialPath)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, after, auth)
			saved, err := os.ReadFile(filepath.Join(f.srcHome, "saved-sessions", "2026", "11", "07", filepath.Base(f.rollout)))
			testutil.NoError(t, err)
			testutil.Equal(t, string(saved), string(moveSessionHeader(codexMoveSID, f.worktree)))
		})
	}
}

func TestCodexMoveRejectsSourceComponentAlias(t *testing.T) {
	for _, kind := range []string{"directory", "file"} {
		t.Run(kind, func(t *testing.T) {
			f := newCodexMoveFixture(t)
			sessions := filepath.Join(f.srcHome, "sessions")
			alias := filepath.Join(sessions, "alias.jsonl")
			if kind == "directory" {
				alias = filepath.Join(sessions, "aliased-date")
				testutil.NoError(t, os.Symlink(filepath.Join("2026", "11", "07"), alias))
				alias = filepath.Join(alias, filepath.Base(f.rollout))
			} else {
				testutil.NoError(t, os.Symlink(filepath.Join("2026", "11", "07", filepath.Base(f.rollout)), alias))
			}
			execMoveSQL(t, filepath.Join(f.srcHome, codexStateDB), `UPDATE threads SET rollout_path=?`, alias)
			before := fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB))
			_, _, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
			testutil.ErrorIs(t, err, ErrAccount)
			got, err := f.d.Get(f.task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Account, "")
			testutil.DeepEqual(t, fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB)), before)
			_, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
			testutil.False(t, found)
		})
	}
}

func TestCodexMoveRejectsTargetSessionsSelfAlias(t *testing.T) {
	f := newCodexMoveFixture(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(f.dstHome, "auth.json"), []byte("target secret"), 0600))
	testutil.NoError(t, os.Symlink(".", filepath.Join(f.dstHome, "sessions")))
	before := fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB))
	_, _, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
	testutil.ErrorIs(t, err, ErrAccount)
	auth, err := os.ReadFile(filepath.Join(f.dstHome, "auth.json"))
	testutil.NoError(t, err)
	testutil.Equal(t, string(auth), "target secret")
	got, err := f.d.Get(f.task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Account, "")
	testutil.DeepEqual(t, fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB)), before)
	_, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
	testutil.False(t, found)
	_, err = os.Stat(filepath.Join(f.dstHome, "2026"))
	testutil.True(t, os.IsNotExist(err))
}

func TestCodexMoveAllowsDistinctAccountHomeAlias(t *testing.T) {
	f := newCodexMoveFixture(t)
	physical := filepath.Join(t.TempDir(), "codex-home")
	testutil.NoError(t, os.Rename(f.srcHome, physical))
	testutil.NoError(t, os.Symlink(physical, f.srcHome))
	before := fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB))
	_, res, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
	testutil.NoError(t, err)
	testutil.True(t, res.Resumed)
	testutil.DeepEqual(t, fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB)), before)
	_, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
	testutil.True(t, found)
}
