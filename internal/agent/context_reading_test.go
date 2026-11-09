package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestReadCodexContext_Errors(t *testing.T) {
	t.Run("nil task", func(t *testing.T) {
		_, ok, err := readCodexContext(nil, config.Config{})
		testutil.False(t, ok)
		testutil.True(t, err != nil)
	})
	t.Run("no index is unmeasured", func(t *testing.T) {
		t.Setenv("CODEX_SQLITE_HOME", t.TempDir())
		_, ok, err := readCodexContext(&model.Task{Worktree: "/w"}, config.Config{})
		testutil.NoError(t, err)
		testutil.False(t, ok)
	})
	t.Run("empty worktree errors", func(t *testing.T) {
		t.Setenv("CODEX_SQLITE_HOME", t.TempDir())
		_, _, err := readCodexContext(&model.Task{}, config.Config{})
		testutil.True(t, err != nil)
	})
	t.Run("family registered", func(t *testing.T) {
		f := FamilyOf("codex")
		testutil.True(t, f.ContextReader != nil)
		testutil.Equal(t, f.ContextProvider, "openai")
		testutil.True(t, FamilyOf("claude").ContextReader == nil)
	})
}
