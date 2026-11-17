package hera

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// An unresolvable project/branch must fail closed, never read as landed.
func TestNewGitCheck_FailsClosedWhenUnconfirmable(t *testing.T) {
	check := NewGitCheck(func() config.Config { return config.Config{} })
	ok, reason := check(&model.Task{ID: "t", Project: "missing", Branch: "b", Worktree: t.TempDir()})
	testutil.Equal(t, ok, false)
	if reason == "" {
		t.Fatal("expected a refusal reason")
	}
}

// landedRepo builds a repo whose default branch contains task branch "b" (a
// local ancestor => merge-safety Tier A Safe) and returns a config mapping a
// project to it. The repo dir doubles as the task worktree.
func landedRepo(t *testing.T) (config.Config, *model.Task) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "master")
	run("commit", "-q", "--allow-empty", "-m", "init")
	run("branch", "b")
	cfg := config.Config{Projects: map[string]config.Project{"proj": {Path: dir, Branch: "master"}}}
	return cfg, &model.Task{ID: "t1", Project: "proj", Branch: "b", Worktree: dir}
}

func TestNewGitCheck_CleanLandedVsDirty(t *testing.T) {
	cfg, task := landedRepo(t)
	check := NewGitCheck(func() config.Config { return cfg })

	ok, reason := check(task)
	if !ok {
		t.Fatalf("clean landed worktree should pass, got reason %q", reason)
	}

	if err := os.WriteFile(filepath.Join(task.Worktree, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason = check(task)
	testutil.Equal(t, ok, false)
	testutil.Contains(t, reason, "uncommitted")
}

// An unlanded branch (not an ancestor of the default) with a CLEAN worktree
// must refuse on the classifier, not on the dirty check ("Open PR refuses").
func TestNewGitCheck_UnlandedBranchCleanWorktreeRefuses(t *testing.T) {
	cfg, task := landedRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = task.Worktree
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-q", "b")
	run("commit", "-q", "--allow-empty", "-m", "unmerged work") // b is now ahead of master
	run("checkout", "-q", "master")
	check := NewGitCheck(func() config.Config { return cfg })
	ok, reason := check(task)
	testutil.Equal(t, ok, false)
	if strings.Contains(reason, "uncommitted") {
		t.Fatalf("refused on dirty check, not the classifier: %q", reason)
	}
}
