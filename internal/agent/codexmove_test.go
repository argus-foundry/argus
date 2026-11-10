package agent

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

const codexMoveSID = "0198a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"

const codexThreadsDDL = `CREATE TABLE threads (
	id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL, source TEXT NOT NULL, model_provider TEXT NOT NULL,
	cwd TEXT NOT NULL, title TEXT NOT NULL, sandbox_policy TEXT NOT NULL,
	approval_mode TEXT NOT NULL, tokens_used INTEGER NOT NULL DEFAULT 0,
	first_user_message TEXT NOT NULL DEFAULT '', creator_user_id TEXT, extra_local TEXT)`

type codexMoveFixture struct {
	d        *db.DB
	task     *model.Task
	srcHome  string
	dstHome  string
	worktree string
	rollout  string
}

func writeCodexHome(t *testing.T, home string, withDDL bool) string {
	t.Helper()
	testutil.NoError(t, os.MkdirAll(home, 0o700))
	dbPath := filepath.Join(home, codexStateDB)
	conn, err := sql.Open("sqlite", dbPath)
	testutil.NoError(t, err)
	defer func() { _ = conn.Close() }()
	if withDDL {
		_, err = conn.Exec(codexThreadsDDL)
	} else {
		_, err = conn.Exec(`CREATE TABLE unrelated (x INTEGER)`)
	}
	testutil.NoError(t, err)
	return dbPath
}

func newCodexMoveFixture(t *testing.T) *codexMoveFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CODEX_SQLITE_HOME", "")
	stubAccountUsage(t, nil)
	orig := codexMoveLoginCheck
	codexMoveLoginCheck = func(context.Context, string) (bool, error) { return true, nil }
	t.Cleanup(func() { codexMoveLoginCheck = orig })

	acctRoot := t.TempDir()
	d := accountFileDB(t, t.TempDir(),
		"[accounts.cx2]\ncodex_home = \""+filepath.Join(acctRoot, "cx2")+"\"\n"+
			"[accounts.cxdup]\ncodex_home = \""+filepath.Join(acctRoot, "cx2")+"\"\n")
	cfg := d.Config()
	srcHome := filepath.Join(home, ".codex")
	dstHome, _, err := cfg.CodexHome("cx")
	testutil.NoError(t, err)

	srcDB := writeCodexHome(t, srcHome, true)
	writeCodexHome(t, dstHome, true)
	testutil.NoError(t, os.WriteFile(filepath.Join(srcHome, "auth.json"), []byte(`{"secret":"x"}`), 0o600))

	wt := t.TempDir()
	rollout := filepath.Join(srcHome, "sessions", "2026", "11", "07", "rollout-2026-11-07T10-00-00-"+codexMoveSID+".jsonl")
	testutil.NoError(t, os.MkdirAll(filepath.Dir(rollout), 0o700))
	testutil.NoError(t, os.WriteFile(rollout, moveSessionHeader(codexMoveSID, wt), 0o600))
	conn, err := sql.Open("sqlite", srcDB)
	testutil.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.Exec(`INSERT INTO threads(id,rollout_path,created_at,updated_at,source,model_provider,cwd,title,sandbox_policy,approval_mode,tokens_used,first_user_message,creator_user_id,extra_local)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		codexMoveSID, rollout, 100, 200, "cli", "openai", wt, "T", "sbx", "never", 42, "hello", "user-src", "keep-local")
	testutil.NoError(t, err)

	task := &model.Task{ID: "cxmv", Name: "cxmover", Project: "proj", Backend: "codex", Worktree: wt, SessionID: codexMoveSID, Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	return &codexMoveFixture{d: d, task: task, srcHome: srcHome, dstHome: dstHome, worktree: wt, rollout: rollout}
}

func codexThreadRow(t *testing.T, dbPath string) (rollout, creator string, tokens int, found bool) {
	t.Helper()
	conn, err := sql.Open("sqlite", dbPath)
	testutil.NoError(t, err)
	defer func() { _ = conn.Close() }()
	var cu sql.NullString
	err = conn.QueryRow(`SELECT rollout_path, creator_user_id, tokens_used FROM threads WHERE id = ?`, codexMoveSID).Scan(&rollout, &cu, &tokens)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 0, false
	}
	testutil.NoError(t, err)
	return rollout, cu.String, tokens, true
}

func TestMoveTaskAccount_CodexCarriesConversation(t *testing.T) {
	f := newCodexMoveFixture(t)
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
	task, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: "cx"})
	testutil.NoError(t, err)
	testutil.True(t, res.Resumed)
	testutil.True(t, res.Started)
	testutil.Equal(t, res.To, "cx")

	stored, _ := f.d.Get("cxmv")
	testutil.Equal(t, stored.Account, "cx")
	testutil.Equal(t, stored.SessionID, codexMoveSID)
	testutil.Equal(t, stored.Worktree, f.worktree)
	testutil.Equal(t, task.Account, "cx")
	testutil.NotNil(t, r.kicked)
	testutil.Equal(t, r.kicked.Account, "cx")
	testutil.Equal(t, r.kicked.SessionID, codexMoveSID)

	rollout, creator, tokens, ok := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
	testutil.True(t, ok)
	testutil.Equal(t, rollout, filepath.Join(f.dstHome, "sessions", "2026", "11", "07", filepath.Base(f.rollout)))
	testutil.Equal(t, creator, "")
	testutil.Equal(t, tokens, 42)
	body, err := os.ReadFile(rollout)
	testutil.NoError(t, err)
	testutil.Equal(t, string(body), string(moveSessionHeader(codexMoveSID, f.worktree)))

	_, statErr := os.Stat(filepath.Join(f.dstHome, "auth.json"))
	testutil.True(t, os.IsNotExist(statErr))
	_, statErr = os.Stat(f.rollout)
	testutil.NoError(t, statErr)
	_, srcCreator, _, srcOK := codexThreadRow(t, filepath.Join(f.srcHome, codexStateDB))
	testutil.True(t, srcOK)
	testutil.Equal(t, srcCreator, "user-src")
}

func TestMoveTaskAccount_CodexMoveBackOverwritesOlderCopy(t *testing.T) {
	f := newCodexMoveFixture(t)
	r := &moveRunner{}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: "cx"})
	testutil.NoError(t, err)
	testutil.NoError(t, os.WriteFile(f.rollout, []byte("newer\n"), 0o600))
	// Move the task back from cx to default: the default home already holds
	// the thread, and the copy replaces it atomically.
	_, _, err = MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: "default"})
	testutil.NoError(t, err)
	body, err := os.ReadFile(f.rollout)
	testutil.NoError(t, err)
	testutil.Equal(t, string(body), string(moveSessionHeader(codexMoveSID, f.worktree)))
}

func TestMoveTaskAccount_CodexRejectionsKeepSource(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*codexMoveFixture)
		wantMsg string
	}{
		{"target not signed in", func(*codexMoveFixture) {
			codexMoveLoginCheck = func(context.Context, string) (bool, error) { return false, nil }
		}, "not signed in"},
		{"target has no state database", func(f *codexMoveFixture) {
			testutil.NoError(t, os.Remove(filepath.Join(f.dstHome, codexStateDB)))
		}, "Codex state database"},
		{"source and target share a home", func(f *codexMoveFixture) {
			f.task.Account = "cx2"
			testutil.NoError(t, f.d.Update(f.task))
		}, "same Codex home"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCodexMoveFixture(t)
			tc.setup(f)
			target := "cx"
			if tc.name == "source and target share a home" {
				target = "cxdup"
			}
			before, _ := f.d.Get("cxmv")
			r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
			_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: target})
			testutil.ErrorIs(t, err, ErrAccount)
			testutil.Contains(t, err.Error(), tc.wantMsg)
			after, _ := f.d.Get("cxmv")
			testutil.Equal(t, after.Account, before.Account)
			testutil.Equal(t, after.SessionID, codexMoveSID)
			testutil.Nil(t, r.kicked)
			_, statErr := os.Stat(f.rollout)
			testutil.NoError(t, statErr)
		})
	}
}

func TestMoveTaskAccount_CodexBusyNeedsForce(t *testing.T) {
	f := newCodexMoveFixture(t)
	r := &moveRunner{sess: &moveSession{idle: false, alive: true}}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: "cx"})
	testutil.ErrorIs(t, err, ErrAccountMoveBusy)
	stored, _ := f.d.Get("cxmv")
	testutil.Equal(t, stored.Account, "")
	_, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
	testutil.False(t, found)

	_, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: "cx", Force: true})
	testutil.NoError(t, err)
	testutil.True(t, res.Resumed)
}

func TestMoveTaskAccount_CodexMissingSourceThreadMovesWithoutResume(t *testing.T) {
	f := newCodexMoveFixture(t)
	testutil.NoError(t, os.Remove(f.rollout))
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
	_, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: "cx"})
	testutil.NoError(t, err)
	testutil.False(t, res.Resumed)
	testutil.NotNil(t, r.revived)
	testutil.Nil(t, r.kicked)
	_, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
	testutil.False(t, found)
}

func TestMoveTaskAccount_CodexKickFailureKeepsAccount(t *testing.T) {
	f := newCodexMoveFixture(t)
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}, kickErr: errors.New("boom")}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: "cx"})
	testutil.ErrorIs(t, err, ErrAccountMoveRestartUnknown)
	stored, _ := f.d.Get("cxmv")
	testutil.Equal(t, stored.Account, "cx")
	testutil.Equal(t, stored.SessionID, codexMoveSID)
	_, statErr := os.Stat(f.rollout)
	testutil.NoError(t, statErr)
}

func TestCodexTransfer_IndexFailureRemovesNewCopy(t *testing.T) {
	f := newCodexMoveFixture(t)
	execMoveSQL(t, filepath.Join(f.dstHome, codexStateDB), `CREATE TRIGGER refuse_insert BEFORE INSERT ON threads BEGIN SELECT RAISE(FAIL, 'write refused'); END`)
	r := &moveRunner{}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: "cx"})
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "index thread")
	_, statErr := os.Stat(filepath.Join(f.dstHome, "sessions", "2026", "11", "07", filepath.Base(f.rollout)))
	testutil.True(t, os.IsNotExist(statErr))
	stored, _ := f.d.Get("cxmv")
	testutil.Equal(t, stored.Account, "")
}

func TestCodexTransfer_UnknownLoginDoesNotBlock(t *testing.T) {
	f := newCodexMoveFixture(t)
	codexMoveLoginCheck = func(context.Context, string) (bool, error) { return false, errCodexLoginUnknown }
	_, res, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: "cxmv", Account: "cx"})
	testutil.NoError(t, err)
	testutil.True(t, res.Resumed)
}

func TestAccountMoveSupported(t *testing.T) {
	cases := map[string]bool{"claude": true, "codex": true, "pi": false, "opencode": false, "sh -c x": false}
	for cmd, want := range cases {
		t.Run(cmd, func(t *testing.T) {
			testutil.Equal(t, AccountMoveSupported(cmd), want)
		})
	}
}

func TestMoveTaskAccount_CodexSameAccountRestartsInPlace(t *testing.T) {
	f := newCodexMoveFixture(t)
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
	_, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "cxmv", Account: "default"})
	testutil.NoError(t, err)
	testutil.True(t, res.Resumed)
	testutil.True(t, res.Started)
	testutil.NotNil(t, r.kicked)
	_, _, _, copied := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
	testutil.False(t, copied)

	testutil.True(t, codexThreadResumable(filepath.Join(f.srcHome, codexStateDB), codexMoveSID))
	testutil.False(t, codexThreadResumable(filepath.Join(f.srcHome, codexStateDB), "not-a-uuid"))
	testutil.False(t, codexThreadResumable(filepath.Join(f.dstHome, codexStateDB), codexMoveSID))
}
