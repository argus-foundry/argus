package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// bmRunner adds the recycle path a backend move restarts through.
type bmRunner struct {
	moveRunner
	recycled   *model.Task
	recycleErr error
}

func (r *bmRunner) Recycle(task *model.Task, _ config.Config, rows, cols uint16) error {
	r.rows, r.cols = rows, cols
	if r.recycleErr != nil {
		return r.recycleErr
	}
	cp := *task
	r.recycled = &cp
	return nil
}

const claudeLines = `{"type":"user","message":{"role":"user","content":"please finish the parser"}}` + "\n" +
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"parser half done"}]}}` + "\n"

func bmFixture(t *testing.T) *moveFixture {
	t.Helper()
	f := newMoveFixture(t, false)
	writeTranscript(t, filepath.Join(f.home, ".claude"), f.worktree, "sid-1", claudeLines)
	f.task.Prompt = "ORIGINAL MISSION"
	f.task.Model = "opus"
	f.task.Branch = "argus/x"
	testutil.NoError(t, f.d.Update(f.task))
	return f
}

func TestMoveTaskBackend_LiveSessionRecycledOnCodexWithHandoff(t *testing.T) {
	f := bmFixture(t)
	r := &bmRunner{moveRunner: moveRunner{sess: &moveSession{idle: true, alive: true}}}

	task, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Backend: "codex"})
	testutil.NoError(t, err)
	testutil.Equal(t, res.FromBackend, "test")
	testutil.Equal(t, res.ToBackend, "codex")
	testutil.True(t, res.Handoff)
	testutil.False(t, res.ConversationCarried)
	testutil.False(t, res.Resumed)
	testutil.True(t, res.Started)
	testutil.Equal(t, task.Backend, "codex")

	got, _ := f.d.Get("mv")
	testutil.Equal(t, got.Backend, "codex")
	testutil.Equal(t, got.SessionID, "") // Codex captures after launch
	testutil.Equal(t, got.Model, "")
	testutil.Equal(t, got.Account, "")
	testutil.Equal(t, got.Worktree, f.worktree) // same task row/worktree

	testutil.NotNil(t, r.recycled)
	testutil.Equal(t, r.recycled.Backend, "codex")
	testutil.Equal(t, r.recycled.SessionID, "")
	seed := r.recycled.Prompt
	testutil.Contains(t, seed, "moved from Claude to Codex")
	testutil.Contains(t, seed, "did NOT carry over")
	testutil.Contains(t, seed, "please finish the parser")
	testutil.Contains(t, seed, "parser half done")
	testutil.Contains(t, seed, "argus/x")
	testutil.Contains(t, seed, f.worktree)
	testutil.Contains(t, seed, "ORIGINAL MISSION")
	testutil.Equal(t, got.Prompt, seed)
	testutil.Nil(t, r.kicked)
}

func TestMoveTaskBackend_UnreadableTranscriptFailsOpen(t *testing.T) {
	f := newMoveFixture(t, false) // no transcript on disk
	r := &bmRunner{moveRunner: moveRunner{sess: &moveSession{idle: true, alive: true}}}
	_, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Backend: "codex"})
	testutil.NoError(t, err)
	testutil.True(t, res.Started)
	testutil.Contains(t, r.recycled.Prompt, "No excerpt of the last exchange")
}

func TestMoveTaskBackend_NoLiveSessionPersistsOnly(t *testing.T) {
	f := bmFixture(t)
	_, res, err := MoveTaskAccount(f.d, &bmRunner{}, AccountMoveInput{TaskID: "mv", Backend: "codex"})
	testutil.NoError(t, err)
	testutil.False(t, res.Started)
	got, _ := f.d.Get("mv")
	testutil.Equal(t, got.Backend, "codex")
	testutil.Contains(t, got.Prompt, "did NOT carry over")
}

func TestMoveTaskBackend_RepeatedMovesDoNotNestSeeds(t *testing.T) {
	f := bmFixture(t)
	r := &bmRunner{}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Backend: "codex"})
	testutil.NoError(t, err)
	// Move back to Claude (default account, backend "test").
	_, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Backend: "test"})
	testutil.NoError(t, err)
	testutil.Equal(t, res.FromBackend, "codex")
	testutil.Equal(t, res.ToBackend, "test")
	got, _ := f.d.Get("mv")
	testutil.Equal(t, got.Backend, "test")
	testutil.Contains(t, got.Prompt, "moved from Codex to Claude")
	testutil.Contains(t, got.Prompt, "ORIGINAL MISSION")
	testutil.Equal(t, strings.Count(got.Prompt, "did NOT carry over"), 1)
	testutil.True(t, got.SessionID != "") // Claude pins a fresh ID
}

func TestMoveTaskBackend_RestartFailureRollsBackEveryField(t *testing.T) {
	f := bmFixture(t)
	r := &bmRunner{moveRunner: moveRunner{sess: &moveSession{idle: true, alive: true}}, recycleErr: errors.New("boom")}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Backend: "codex"})
	testutil.Error(t, err)
	got, _ := f.d.Get("mv")
	testutil.Equal(t, got.Backend, "test")
	testutil.Equal(t, got.Model, "opus")
	testutil.Equal(t, got.SessionID, "sid-1")
	testutil.Equal(t, got.Prompt, "ORIGINAL MISSION")
}

func TestMoveTaskBackend_SessionGoneBeforeRestartLeavesMoveForCaller(t *testing.T) {
	f := bmFixture(t)
	sess := &moveSession{idle: true, alive: true}
	r := &bmRunner{moveRunner: moveRunner{sess: sess}, recycleErr: errors.New("gone")}
	// Recycle error with a now-dead session: treat like account move.
	rr := &deadOnRecycle{bmRunner: r}
	_, res, err := MoveTaskAccount(f.d, rr, AccountMoveInput{TaskID: "mv", Backend: "codex"})
	testutil.NoError(t, err)
	testutil.False(t, res.Started)
	got, _ := f.d.Get("mv")
	testutil.Equal(t, got.Backend, "codex")
}

type deadOnRecycle struct{ *bmRunner }

func (r *deadOnRecycle) Recycle(task *model.Task, cfg config.Config, rows, cols uint16) error {
	r.sess.alive = false
	return r.bmRunner.Recycle(task, cfg, rows, cols)
}

func TestMoveTaskBackend_Rejections(t *testing.T) {
	f := bmFixture(t)
	busy := &bmRunner{moveRunner: moveRunner{sess: &moveSession{idle: false, alive: true}}}
	cases := []struct {
		name   string
		in     AccountMoveInput
		runner accountMoveRunner
		want   error
	}{
		{"busy", AccountMoveInput{TaskID: "mv", Backend: "codex"}, busy, ErrAccountMoveBusy},
		{"same family", AccountMoveInput{TaskID: "mv", Backend: "test"}, &bmRunner{}, ErrAccount},
		{"unknown backend", AccountMoveInput{TaskID: "mv", Backend: "nope"}, &bmRunner{}, ErrAccount},
		{"unsupported kind", AccountMoveInput{TaskID: "mv", Backend: "pi"}, &bmRunner{}, ErrAccount},
		{"account lacks codex", AccountMoveInput{TaskID: "mv", Backend: "codex", Account: "work"}, &bmRunner{}, ErrAccount},
		{"auto not allowed", AccountMoveInput{TaskID: "mv", Backend: "codex", Account: "auto"}, &bmRunner{}, ErrAccount},
		{"pending", AccountMoveInput{TaskID: "mv", Backend: "codex"}, &bmRunner{moveRunner: moveRunner{pending: true}}, ErrAccountMoveBusy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := MoveTaskAccount(f.d, tc.runner, tc.in)
			testutil.ErrorIs(t, err, tc.want)
			got, _ := f.d.Get("mv")
			testutil.Equal(t, got.Backend, "test") // no side effects
			testutil.Equal(t, got.Prompt, "ORIGINAL MISSION")
		})
	}
}

func TestMoveTaskBackend_LiveSessionNeedsRecycle(t *testing.T) {
	f := bmFixture(t)
	r := &noRecycleMoveRunner{runner: &moveRunner{sess: &moveSession{idle: true, alive: true}}}
	_, _, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Backend: "codex"})
	testutil.Error(t, err)
	got, _ := f.d.Get("mv")
	testutil.Equal(t, got.Backend, "test")
}

func TestMoveTaskBackend_CodexAccountTarget(t *testing.T) {
	f := bmFixture(t)
	r := &bmRunner{moveRunner: moveRunner{sess: &moveSession{idle: true, alive: true}}}
	_, res, err := MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Backend: "codex", Account: "cx"})
	testutil.NoError(t, err)
	testutil.Equal(t, res.To, "cx")
	got, _ := f.d.Get("mv")
	testutil.Equal(t, got.Account, "cx")
	testutil.False(t, got.AccountRouted)
}

func TestHandoffGitStatus(t *testing.T) {
	repo := initGitRepo(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644))
	out, ok := handoffGitStatus(repo)
	testutil.True(t, ok)
	testutil.Contains(t, out, "new.txt")

	// A non-repo fails open.
	if _, err := exec.LookPath("git"); err == nil {
		_, ok = handoffGitStatus(t.TempDir())
		testutil.False(t, ok)
	}
	_, ok = handoffGitStatus("")
	testutil.False(t, ok)
}

// The resident's permission policy is Claude-only, so a backend move must
// refuse it before touching the row (add-resident-agent).
func TestMoveTaskBackend_ResidentRejected(t *testing.T) {
	f := bmFixture(t)
	res, _, err := f.d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	r := &bmRunner{}
	_, _, err = MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: res.ID, Backend: "codex"})
	testutil.ErrorIs(t, err, db.ErrResidentProtected)
	testutil.ErrorIs(t, err, ErrAccount)
	testutil.Nil(t, r.recycled)
	after, _ := f.d.Get(res.ID)
	testutil.Equal(t, after.Backend, res.Backend)
	testutil.Equal(t, after.Prompt, res.Prompt)
}
