package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/drn/argus/internal/codexctx"
	"github.com/drn/argus/internal/testutil"
)

func TestCodexContextFreshOverlayStorage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source := t.TempDir()
	overlay, err := ensureCodexSkills(source)
	testutil.NoError(t, err)
	for _, dir := range []string{"sessions", "archived_sessions"} {
		t.Run(dir, func(t *testing.T) {
			_, err := os.Stat(filepath.Join(source, dir))
			testutil.True(t, os.IsNotExist(err))
			testutil.NoError(t, os.Mkdir(filepath.Join(overlay, dir), 0o700))
			path := filepath.Join(overlay, dir, "rollout-main.jsonl")
			raw := `{"type":"session_meta","payload":{"id":"main","cwd":"/w","timestamp":"2026-11-07T10:00:00Z"}}` + "\n" + `{"timestamp":"2026-11-07T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":10},"model_context_window":100}}}` + "\n"
			testutil.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
			reading, _, ok, err := codexctx.LatestForThread(source, "/w", codexctx.Thread{ID: "main", RolloutPath: path}, overlay)
			testutil.NoError(t, err)
			testutil.True(t, ok)
			testutil.Equal(t, reading.UsedTokens, int64(10))
		})
	}
}
