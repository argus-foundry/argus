package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCodexMoveLiveMissingConversationStartsFresh(t *testing.T) {
	f := newCodexMoveFixture(t)
	testutil.NoError(t, os.Remove(f.rollout))
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
	task, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
	testutil.NoError(t, err)
	testutil.NotNil(t, task)
	if task == nil {
		return
	}
	testutil.False(t, res.Resumed)
	testutil.NotNil(t, r.recycled)
	testutil.Equal(t, task.SessionID, "")
	if r.recycled != nil {
		cmd, cleanup, err := BuildCmd(r.recycled, f.d.Config(), false)
		testutil.NoError(t, err)
		if cleanup != nil {
			defer cleanup()
		}
		if cmd != nil {
			for _, arg := range cmd.Args {
				testutil.False(t, strings.Contains(arg, codexMoveSID))
				testutil.False(t, strings.Contains(arg, " resume "))
			}
		}
	}
}

func TestCodexMoveRefusesUnknownLiveConversation(t *testing.T) {
	f := newCodexMoveFixture(t)
	f.task.SessionID = ""
	testutil.NoError(t, f.d.Update(f.task))
	execMoveSQL(t, filepath.Join(f.srcHome, codexStateDB), `DELETE FROM threads`)
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
	testutil.ErrorIs(t, err, ErrAccount)
	got, err := f.d.Get(f.task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Account, "")
	testutil.Nil(t, r.kicked)
	testutil.Nil(t, r.recycled)
}

func TestCodexMoveFreshLaunchRequiresRecycle(t *testing.T) {
	f := newCodexMoveFixture(t)
	testutil.NoError(t, os.Remove(f.rollout))
	r := &noRecycleMoveRunner{runner: &moveRunner{sess: &moveSession{idle: true, alive: true}}}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
	testutil.ErrorIs(t, err, ErrAccount)
	got, err := f.d.Get(f.task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Account, "")
	testutil.Equal(t, got.SessionID, codexMoveSID)
}

func TestCodexMoveSeparateSQLiteHome(t *testing.T) {
	f := newCodexMoveFixture(t)
	sqliteHome := t.TempDir()
	testutil.NoError(t, copyFile(filepath.Join(f.srcHome, codexStateDB), filepath.Join(sqliteHome, codexStateDB)))
	t.Setenv("CODEX_SQLITE_HOME", sqliteHome)
	f.task.SessionID = ""
	testutil.NoError(t, f.d.Update(f.task))
	_, res, err := MoveTaskAccount(f.d, &moveRunner{sess: &moveSession{idle: true, alive: true}}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
	testutil.NoError(t, err)
	testutil.True(t, res.Resumed)
	rollout, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
	testutil.True(t, found)
	testutil.Equal(t, rollout, filepath.Join(f.dstHome, "sessions", "2026", "11", "07", filepath.Base(f.rollout)))
}
