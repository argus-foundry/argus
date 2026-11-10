package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func moveSessionHeader(id, cwd string) []byte {
	return []byte(fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q,"timestamp":"2026-11-07T10:00:00Z"}}`+"\n", id, cwd))
}

func TestCodexMoveRejectsNonConversationFiles(t *testing.T) {
	cases := []string{"credential hard link", "copied credentials", "valid rollout hard link", "wrong thread", "wrong record type", "missing payload", "malformed JSON", "missing newline", "oversized header"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			f := newCodexMoveFixture(t)
			auth := filepath.Join(f.srcHome, "auth.json")
			switch kind {
			case "credential hard link":
				testutil.NoError(t, os.Remove(f.rollout))
				testutil.NoError(t, os.Link(auth, f.rollout))
			case "copied credentials":
				testutil.NoError(t, copyFile(auth, f.rollout))
			case "valid rollout hard link":
				testutil.NoError(t, os.WriteFile(f.rollout, moveSessionHeader(codexMoveSID, f.worktree), 0600))
				testutil.NoError(t, os.Link(f.rollout, filepath.Join(filepath.Dir(f.rollout), "another.jsonl")))
			case "wrong thread":
				testutil.NoError(t, os.WriteFile(f.rollout, moveSessionHeader("0198a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c", f.worktree), 0600))
			case "wrong record type":
				testutil.NoError(t, os.WriteFile(f.rollout, bytes.Replace(moveSessionHeader(codexMoveSID, f.worktree), []byte("session_meta"), []byte("event_msg"), 1), 0600))
			case "missing payload":
				testutil.NoError(t, os.WriteFile(f.rollout, []byte("{\"type\":\"session_meta\"}\n"), 0600))
			case "malformed JSON":
				testutil.NoError(t, os.WriteFile(f.rollout, []byte("not JSON\n"), 0600))
			case "missing newline":
				testutil.NoError(t, os.WriteFile(f.rollout, bytes.TrimSuffix(moveSessionHeader(codexMoveSID, f.worktree), []byte("\n")), 0600))
			case "oversized header":
				testutil.NoError(t, os.WriteFile(f.rollout, []byte(strings.Repeat(" ", 4<<20)+string(moveSessionHeader(codexMoveSID, f.worktree))), 0600))
			}
			info, err := os.Stat(f.rollout)
			testutil.NoError(t, err)
			stat, ok := info.Sys().(*syscall.Stat_t)
			testutil.True(t, ok)
			if ok {
				expected := uint64(1)
				if kind == "credential hard link" || kind == "valid rollout hard link" {
					expected = 2
				}
				testutil.Equal(t, uint64(stat.Nlink), expected)
			}
			rowBefore := fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB))
			authBefore, err := os.ReadFile(auth)
			testutil.NoError(t, err)
			rolloutBefore, err := os.ReadFile(f.rollout)
			testutil.NoError(t, err)
			_, _, err = MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
			testutil.ErrorIs(t, err, ErrAccount)
			got, err := f.d.Get(f.task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Account, "")
			testutil.Equal(t, got.SessionID, codexMoveSID)
			testutil.DeepEqual(t, fullMoveRow(t, filepath.Join(f.srcHome, codexStateDB)), rowBefore)
			afterAuth, err := os.ReadFile(auth)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, afterAuth, authBefore)
			afterRollout, err := os.ReadFile(f.rollout)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, afterRollout, rolloutBefore)
			_, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
			testutil.False(t, found)
			_, err = os.Stat(filepath.Join(f.dstHome, "sessions"))
			testutil.True(t, os.IsNotExist(err))
		})
	}
}

func TestCodexMoveValidHeaderCopiesEntireRollout(t *testing.T) {
	f := newCodexMoveFixture(t)
	body := append(moveSessionHeader(codexMoveSID, f.worktree), []byte(strings.Repeat(`{"type":"event_msg"}`+"\n", 1000))...)
	testutil.NoError(t, os.WriteFile(f.rollout, body, 0600))
	_, res, err := MoveTaskAccount(f.d, &moveRunner{}, AccountMoveInput{TaskID: f.task.ID, Account: "cx"})
	testutil.NoError(t, err)
	testutil.True(t, res.Resumed)
	dst, _, _, found := codexThreadRow(t, filepath.Join(f.dstHome, codexStateDB))
	testutil.True(t, found)
	copied, err := os.ReadFile(dst)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, copied, body)
}
