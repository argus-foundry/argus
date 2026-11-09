package mergesafety

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskSummary(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	gitRun(t, dir, "checkout", "-b", "worker")
	cfg := config.DefaultConfig()
	cfg.Projects = map[string]config.Project{"p": {Path: dir, Branch: "master"}}
	task := &model.Task{ID: "w", Project: "p", Branch: "worker", Worktree: dir, BaseBranch: "master"}
	ctx := context.Background()
	s := SummarizeTask(ctx, cfg, task, "")
	testutil.True(t, s.Empty())
	testutil.Contains(t, s.PRState, "unknown")
	v, err := ClassifyTask(ctx, cfg, task)
	testutil.NoError(t, err)
	testutil.True(t, v.Safe)
	addCommit(t, dir, "feature", "content")
	// Empty-work safety follows the branch we would delete, even if somebody
	// switched the worktree to the base branch after the worker made commits.
	gitRun(t, dir, "checkout", "master")
	s = SummarizeTask(ctx, cfg, task, "open")
	testutil.False(t, s.Empty())
	testutil.Equal(t, s.CommitsAhead, "1")
	testutil.Contains(t, s.Subjects, "add feature")
	testutil.Equal(t, s.PRState, "open")
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "untracked\nname"), []byte("x"), 0600))
	s = SummarizeTask(ctx, cfg, task, "")
	testutil.Equal(t, s.DirtyCount, "1")
	task.BaseBranch = "missing"
	s = SummarizeTask(ctx, cfg, task, "")
	testutil.Contains(t, s.CommitsAhead, "unknown")
	testutil.False(t, s.Empty())
	task.Worktree = ""
	s = SummarizeTask(ctx, cfg, task, "")
	testutil.Contains(t, s.DirtyCount, "unknown")
	testutil.False(t, s.Empty())
}

func TestCandidateForTaskUnresolvable(t *testing.T) {
	cfg := config.DefaultConfig()
	task := &model.Task{ID: "w", Branch: "feature"}
	c := CandidateForTask(context.Background(), cfg, task)
	testutil.Equal(t, c.ID, "w")
	testutil.Equal(t, c.RepoDir, "")
	v, err := ClassifyTask(context.Background(), cfg, task)
	testutil.NoError(t, err)
	testutil.False(t, v.Safe)
	task.Kind = model.KindResident
	v, err = ClassifyTask(context.Background(), cfg, task)
	testutil.NoError(t, err)
	testutil.False(t, v.Safe)
	testutil.Contains(t, v.Reason, "protected")
}
