package agent

import (
	"database/sql"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func execMoveSQL(t *testing.T, path, query string, args ...any) {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	testutil.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.Exec(query, args...)
	testutil.NoError(t, err)
}

func TestCodexMoveFirstLiveConversation(t *testing.T) {
	f := newCodexMoveFixture(t)
	f.task.SessionID = ""
	testutil.NoError(t, f.d.Update(f.task))
	sid, err := CaptureSessionID(f.task, f.d.Config())
	testutil.NoError(t, err)
	testutil.Equal(t, sid, codexMoveSID)
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
	_, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
	testutil.NoError(t, err)
	testutil.True(t, res.Resumed)
	_, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
	testutil.True(t, found)
	stored, err := f.d.Get(f.task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, stored.SessionID, codexMoveSID)
	if r.kicked != nil {
		testutil.Equal(t, r.kicked.SessionID, codexMoveSID)
	} else {
		t.Error("conversation was not kicked with resume")
	}
}

func TestCodexMoveTargetReadinessMatrix(t *testing.T) {
	for _, source := range []string{"empty ID", "missing row", "missing rollout"} {
		for _, target := range []string{"missing database", "invalid database", "missing table", "missing columns"} {
			t.Run(source+"/"+target, func(t *testing.T) {
				f := newCodexMoveFixture(t)
				srcDB, dstDB := filepath.Join(f.srcHome, codexStateDB), filepath.Join(f.dstHome, codexStateDB)
				switch source {
				case "empty ID":
					f.task.SessionID = ""
					testutil.NoError(t, f.d.Update(f.task))
				case "missing row":
					execMoveSQL(t, srcDB, `DELETE FROM threads`)
				case "missing rollout":
					testutil.NoError(t, os.Remove(f.rollout))
				}
				testutil.NoError(t, os.Remove(dstDB))
				switch target {
				case "invalid database":
					testutil.NoError(t, os.WriteFile(dstDB, []byte("bad SQLite"), 0600))
				case "missing table":
					writeCodexHome(t, f.dstHome, false)
				case "missing columns":
					execMoveSQL(t, dstDB, `CREATE TABLE threads(id TEXT PRIMARY KEY)`)
				}
				before, err := f.d.Get(f.task.ID)
				testutil.NoError(t, err)
				_, _, err = MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
				testutil.ErrorIs(t, err, ErrAccount)
				if err != nil {
					testutil.Contains(t, err.Error(), "Codex state database")
				}
				after, err := f.d.Get(f.task.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, after.Account, before.Account)
				testutil.Equal(t, after.SessionID, before.SessionID)
			})
		}
	}
}

func TestCodexMoveRejectsUnsafeSource(t *testing.T) {
	for _, kind := range []string{"credentials", "outside file", "symlink to credentials", "named pipe"} {
		t.Run(kind, func(t *testing.T) {
			f := newCodexMoveFixture(t)
			bad := filepath.Join(f.srcHome, "auth.json")
			switch kind {
			case "named pipe":
				bad = filepath.Join(filepath.Dir(f.rollout), "fifo.jsonl")
				testutil.NoError(t, syscall.Mkfifo(bad, 0600))
			case "outside file":
				bad = filepath.Join(t.TempDir(), "rollout.jsonl")
				testutil.NoError(t, os.WriteFile(bad, []byte("outside"), 0600))
			case "symlink to credentials":
				bad = filepath.Join(filepath.Dir(f.rollout), "bad.jsonl")
				testutil.NoError(t, os.Symlink(filepath.Join(f.srcHome, "auth.json"), bad))
			}
			execMoveSQL(t, filepath.Join(f.srcHome, codexStateDB), `UPDATE threads SET rollout_path=?`, bad)
			_, _, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
			testutil.ErrorIs(t, err, ErrAccount)
			got, err := f.d.Get(f.task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Account, "")
			_, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
			testutil.False(t, found)
		})
	}
}

func TestCodexMoveRejectsDestinationSymlink(t *testing.T) {
	for _, component := range []string{"sessions", "sessions/2026/11"} {
		t.Run(component, func(t *testing.T) {
			f := newCodexMoveFixture(t)
			outside := t.TempDir()
			rel := filepath.Join("2026", "11", "07", filepath.Base(f.rollout))
			if component != "sessions" {
				rel = filepath.Join("07", filepath.Base(f.rollout))
			}
			victim := filepath.Join(outside, rel)
			testutil.NoError(t, os.MkdirAll(filepath.Dir(victim), 0700))
			testutil.NoError(t, os.WriteFile(victim, []byte("target-only history"), 0600))
			link := filepath.Join(f.dstHome, component)
			testutil.NoError(t, os.MkdirAll(filepath.Dir(link), 0700))
			testutil.NoError(t, os.Symlink(outside, link))
			_, _, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
			testutil.ErrorIs(t, err, ErrAccount)
			body, err := os.ReadFile(victim)
			testutil.NoError(t, err)
			testutil.Equal(t, string(body), "target-only history")
			got, err := f.d.Get(f.task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Account, "")
		})
	}
}

func TestCodexMoveRejectsPhysicalHomeAlias(t *testing.T) {
	f := newCodexMoveFixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	testutil.NoError(t, os.Symlink(f.srcHome, alias))
	cfg := f.d.Config()
	account := cfg.Accounts["cx"]
	account.CodexHome = alias
	cfg.Accounts["cx"] = account
	to := *f.task
	to.Account = "cx"
	before, err := os.ReadFile(f.rollout)
	testutil.NoError(t, err)
	_, err = codexTransferSession(f.task, &to, cfg)
	testutil.ErrorIs(t, err, ErrAccount)
	path, _, _, _ := codexThreadRow(t, filepath.Join(f.srcHome, codexStateDB))
	testutil.Equal(t, path, f.rollout)
	after, err := os.ReadFile(f.rollout)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, after, before)
}
