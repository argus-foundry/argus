package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func TestBuildCmd_ResidentWorkdirPrecondition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resDir := db.ResidentWorkDir()
	testutil.NoError(t, os.MkdirAll(resDir, 0o755))
	other := t.TempDir()

	t.Run("resident with the resident dir starts", func(t *testing.T) {
		task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: resDir}
		cmd, cleanup, err := BuildCmd(task, testConfig(), false)
		testutil.NoError(t, err)
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
		testutil.Equal(t, cmd.Dir, resDir)
	})

	t.Run("resident with any other dir refuses", func(t *testing.T) {
		task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: other}
		_, _, err := BuildCmd(task, testConfig(), false)
		if err == nil || !strings.Contains(err.Error(), "not the resident working directory") {
			t.Fatalf("want resident-dir refusal, got %v", err)
		}
	})

	t.Run("resident with empty worktree refuses", func(t *testing.T) {
		task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident}
		_, _, err := BuildCmd(task, testConfig(), false)
		if err == nil || !strings.Contains(err.Error(), "no worktree set") {
			t.Fatalf("want no-worktree refusal, got %v", err)
		}
	})

	t.Run("non-resident with empty worktree still refuses", func(t *testing.T) {
		task := &model.Task{ID: "p", Name: "plain"}
		_, _, err := BuildCmd(task, testConfig(), false)
		if err == nil || !strings.Contains(err.Error(), "no worktree set") {
			t.Fatalf("want no-worktree refusal, got %v", err)
		}
	})
}

// resume.go and bgsessionreap.go read task.Worktree; the resident's is
// non-empty by design so neither needs a resident branch.
func TestResidentWorktreeNonEmptyKeepsResumeHelpersWorking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	task := &model.Task{ID: "r", Kind: model.KindResident, Worktree: db.ResidentWorkDir(), SessionID: "s1"}
	if got := reapBackgroundSessionForResume(task, testConfig()); len(got) > 0 {
		t.Fatalf("unexpected reap result %v", got)
	}
}
