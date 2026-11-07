package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/drn/argus/internal/codexctx"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

const usageFixture = `{"timestamp":"2026-11-07T10:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}
{"timestamp":"2026-11-07T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":2},"last_token_usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":2}}}}
`

func TestReadCodexUsage_Errors(t *testing.T) {
	t.Run("nil task", func(t *testing.T) {
		_, ok, err := readCodexUsage(nil, config.Config{})
		testutil.False(t, ok)
		testutil.True(t, err != nil)
	})
	t.Run("no index is unmeasured", func(t *testing.T) {
		t.Setenv("CODEX_SQLITE_HOME", t.TempDir())
		_, ok, err := readCodexUsage(&model.Task{Worktree: "/w"}, config.Config{})
		testutil.NoError(t, err)
		testutil.False(t, ok)
	})
	t.Run("family registered", func(t *testing.T) {
		f := FamilyOf("codex")
		testutil.True(t, f.UsageReader != nil)
		testutil.Equal(t, f.UsageSource, "codex_rollout")
		testutil.True(t, FamilyOf("claude").UsageReader == nil)
	})
}

func usageOfPath(t *testing.T, p string) (codexctx.SessionUsage, bool, error) {
	t.Helper()
	f, err := os.Open(p)
	testutil.NoError(t, err)
	defer f.Close() //nolint:errcheck // test handle
	return codexUsageOf(f, p)
}

func TestCodexUsageOf_Cache(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rollout-a.jsonl")
	testutil.NoError(t, os.WriteFile(p, []byte(usageFixture), 0o600))
	u, ok, err := usageOfPath(t, p)
	testutil.NoError(t, err)
	testutil.True(t, ok)
	testutil.Equal(t, *u.Totals.InputTokens, int64(10))
	u2, _, _ := usageOfPath(t, p)
	testutil.Equal(t, *u2.Totals.CachedInputTokens, int64(4))

	testutil.NoError(t, os.WriteFile(p, []byte(usageFixture+usageFixture), 0o600))
	u3, _, _ := usageOfPath(t, p)
	testutil.Equal(t, len(u3.Turns), 2)

	usageCacheMu.Lock()
	for i := 0; i < usageCacheMax; i++ {
		usageCache[string(rune('a'+i))] = usageCacheEntry{}
	}
	usageCacheMu.Unlock()
	p2 := filepath.Join(t.TempDir(), "rollout-b.jsonl")
	testutil.NoError(t, os.WriteFile(p2, []byte(usageFixture), 0o600))
	_, ok, _ = usageOfPath(t, p2)
	testutil.True(t, ok)
	usageCacheMu.Lock()
	n := len(usageCache)
	usageCacheMu.Unlock()
	testutil.True(t, n <= usageCacheMax)

	d, err := os.Open(t.TempDir())
	testutil.NoError(t, err)
	defer d.Close() //nolint:errcheck // test handle
	_, _, err = codexUsageOf(d, "rollout-dir.jsonl")
	testutil.True(t, err != nil)
}
