package daemon

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// An unresolvable project/branch must fail closed, never read as landed.
func TestHeraArchiveGitCheck_FailsClosedWhenUnconfirmable(t *testing.T) {
	check := heraArchiveGitCheck(func() config.Config { return config.Config{} })
	ok, reason := check(&model.Task{ID: "t", Project: "missing", Branch: "b", Worktree: t.TempDir()})
	testutil.Equal(t, ok, false)
	if reason == "" {
		t.Fatal("expected a refusal reason")
	}
}
