package codexctx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drn/argus/internal/testutil"
)

func startLine(ts, turn string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"task_started","turn_id":%q}}`+"\n", ts, turn)
}

func usageTokenLine(ts string, ti, tc, to, li, lc, lo int64) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d},"last_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d}}}}`+"\n",
		ts, ti, tc, to, li, lc, lo)
}

func usageOf(t *testing.T, raw string) SessionUsage {
	t.Helper()
	u, ok, err := Usage(strings.NewReader(raw), int64(len(raw)))
	testutil.NoError(t, err)
	testutil.True(t, ok)
	return u
}

func TestUsage(t *testing.T) {
	t.Run("totals from cumulative and turns from per-call sums", func(t *testing.T) {
		raw := startLine("2026-11-07T10:00:00Z", "t1") +
			usageTokenLine("2026-11-07T10:00:01Z", 1000, 500, 100, 1000, 500, 100) +
			usageTokenLine("2026-11-07T10:00:02Z", 1900, 1200, 160, 900, 700, 60) +
			startLine("2026-11-07T10:01:00Z", "t2") +
			usageTokenLine("2026-11-07T10:01:01Z", 2000, 1300, 200, 100, 100, 40)
		u := usageOf(t, raw)
		testutil.Equal(t, *u.Totals.InputTokens, int64(2000))
		testutil.Equal(t, *u.Totals.CachedInputTokens, int64(1300))
		testutil.Equal(t, *u.Totals.OutputTokens, int64(200))
		testutil.Equal(t, u.MeasuredAt, time.Date(2026, 11, 7, 10, 1, 1, 0, time.UTC))
		testutil.Equal(t, len(u.Turns), 2)
		testutil.Equal(t, u.Turns[0].ID, "t1")
		testutil.Equal(t, *u.Turns[0].InputTokens, int64(1900))
		testutil.Equal(t, *u.Turns[0].CachedInputTokens, int64(1200))
		testutil.Equal(t, *u.Turns[0].OutputTokens, int64(160))
		testutil.Equal(t, *u.Turns[1].InputTokens, int64(100))
	})
	t.Run("repeated identical token_count counts once", func(t *testing.T) {
		line := usageTokenLine("2026-11-07T10:00:01Z", 1000, 0, 100, 1000, 0, 100)
		u := usageOf(t, startLine("2026-11-07T10:00:00Z", "t1")+line+line)
		testutil.Equal(t, *u.Turns[0].InputTokens, int64(1000))
	})
	t.Run("measured zero differs from absent", func(t *testing.T) {
		raw := startLine("2026-11-07T10:00:00Z", "t1") + usageTokenLine("2026-11-07T10:00:01Z", 0, 0, 0, 0, 0, 0) +
			startLine("2026-11-07T10:01:00Z", "t2")
		u := usageOf(t, raw)
		testutil.True(t, u.Turns[0].InputTokens != nil)
		testutil.Equal(t, *u.Turns[0].InputTokens, int64(0))
		testutil.True(t, u.Turns[1].InputTokens == nil)
	})
	t.Run("token_count before any turn updates totals only", func(t *testing.T) {
		u := usageOf(t, usageTokenLine("2026-11-07T10:00:01Z", 10, 0, 1, 10, 0, 1))
		testutil.Equal(t, len(u.Turns), 0)
		testutil.Equal(t, *u.Totals.InputTokens, int64(10))
	})
	t.Run("no usable token_count", func(t *testing.T) {
		raw := startLine("2026-11-07T10:00:00Z", "t1") + "garbage\n" +
			`{"timestamp":"2026-11-07T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":null}}` + "\n"
		_, ok, err := Usage(strings.NewReader(raw), int64(len(raw)))
		testutil.NoError(t, err)
		testutil.True(t, !ok)
	})
	t.Run("torn final line ignored", func(t *testing.T) {
		raw := startLine("2026-11-07T10:00:00Z", "t1") + usageTokenLine("2026-11-07T10:00:01Z", 5, 0, 1, 5, 0, 1) + `{"timestamp":"2026-11-07T10:00:02Z","type":"event_msg","payload":{"type":"token_c`
		u := usageOf(t, raw)
		testutil.Equal(t, *u.Totals.InputTokens, int64(5))
	})
}

func TestOpenThreadRollout_Guards(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	testutil.NoError(t, os.MkdirAll(dir, 0o700))
	t.Run("not a rollout name", func(t *testing.T) {
		_, err := OpenThreadRollout(home, Thread{RolloutPath: filepath.Join(dir, "notes.txt")})
		testutil.True(t, err != nil)
	})
	t.Run("outside storage", func(t *testing.T) {
		_, err := OpenThreadRollout(home, Thread{RolloutPath: filepath.Join(t.TempDir(), "rollout-x.jsonl")})
		testutil.True(t, err != nil)
	})
	t.Run("missing", func(t *testing.T) {
		_, err := OpenThreadRollout(home, Thread{RolloutPath: filepath.Join(dir, "rollout-x.jsonl")})
		testutil.True(t, os.IsNotExist(err))
	})
	t.Run("regular file parses", func(t *testing.T) {
		p := filepath.Join(dir, "rollout-ok.jsonl")
		testutil.NoError(t, os.WriteFile(p, []byte(`{"timestamp":"2026-11-07T10:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`+"\n"), 0o600))
		f, err := OpenThreadRollout(home, Thread{RolloutPath: p})
		testutil.NoError(t, err)
		defer f.Close() //nolint:errcheck // test handle
		_, ok, err := UsageInFile(f)
		testutil.NoError(t, err)
		testutil.False(t, ok)
	})
	t.Run("directory is not regular", func(t *testing.T) {
		d, err := os.Open(dir)
		testutil.NoError(t, err)
		defer d.Close() //nolint:errcheck // test handle
		_, _, err = UsageInFile(d)
		testutil.True(t, err != nil)
	})
}
