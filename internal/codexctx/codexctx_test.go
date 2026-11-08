package codexctx

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drn/argus/internal/testutil"
)

func tokenLine(ts string, in, out, reasoning, window int64) string {
	w := ""
	if window > 0 {
		w = fmt.Sprintf(`,"model_context_window":%d`, window)
	}
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":999999,"output_tokens":999999,"total_tokens":1999998},"last_token_usage":{"input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d,"total_tokens":%d}%s}}}`+"\n",
		ts, in, out, reasoning, in+out, w)
}

func TestLatest(t *testing.T) {
	turn := `{"timestamp":"2026-11-07T10:00:00Z","type":"turn_context","payload":{"model":"gpt-a"}}` + "\n"
	t.Run("uses last call not cumulative total", func(t *testing.T) {
		raw := turn + tokenLine("2026-11-07T10:00:01Z", 50000, 1000, 0, 200000)
		r, ok, err := Latest(strings.NewReader(raw), int64(len(raw)))
		testutil.NoError(t, err)
		testutil.True(t, ok)
		testutil.Equal(t, r.UsedTokens, int64(51000))
		testutil.Equal(t, r.WindowTokens, int64(200000))
		testutil.Equal(t, r.Model, "gpt-a")
		testutil.Equal(t, r.MeasuredAt, time.Date(2026, 11, 7, 10, 0, 1, 0, time.UTC))
	})
	t.Run("compaction drops used tokens", func(t *testing.T) {
		raw := turn + tokenLine("2026-11-07T10:00:01Z", 190000, 500, 0, 200000) +
			`{"timestamp":"2026-11-07T10:00:02Z","type":"compacted","payload":{}}` + "\n" +
			tokenLine("2026-11-07T10:00:03Z", 8000, 200, 0, 200000)
		r, ok, _ := Latest(strings.NewReader(raw), int64(len(raw)))
		testutil.True(t, ok)
		testutil.Equal(t, r.UsedTokens, int64(8200))
	})
	t.Run("reasoning total matches Codex meter", func(t *testing.T) {
		raw := tokenLine("2026-11-07T10:00:01Z", 1000, 700, 600, 200000)
		r, _, _ := Latest(strings.NewReader(raw), int64(len(raw)))
		testutil.Equal(t, r.UsedTokens, int64(1700))
	})
	t.Run("missing window stays unknown", func(t *testing.T) {
		raw := tokenLine("2026-11-07T10:00:01Z", 1000, 10, 0, 0)
		r, ok, _ := Latest(strings.NewReader(raw), int64(len(raw)))
		testutil.True(t, ok)
		testutil.Equal(t, r.WindowTokens, int64(0))
		testutil.Equal(t, r.UsedTokens, int64(1010))
	})
	t.Run("measured zero is a reading", func(t *testing.T) {
		raw := tokenLine("2026-11-07T10:00:01Z", 0, 0, 0, 200000)
		r, ok, _ := Latest(strings.NewReader(raw), int64(len(raw)))
		testutil.True(t, ok)
		testutil.Equal(t, r.UsedTokens, int64(0))
	})
	t.Run("no usable event is not found", func(t *testing.T) {
		raw := turn + `{"timestamp":"2026-11-07T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":null}}` + "\nnot json token_count\n"
		_, ok, err := Latest(strings.NewReader(raw), int64(len(raw)))
		testutil.NoError(t, err)
		testutil.False(t, ok)
	})
	t.Run("torn final line ignored", func(t *testing.T) {
		raw := tokenLine("2026-11-07T10:00:01Z", 100, 1, 0, 200000) + `{"timestamp":"2026-11-07T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"last_tok`
		r, ok, _ := Latest(strings.NewReader(raw), int64(len(raw)))
		testutil.True(t, ok)
		testutil.Equal(t, r.UsedTokens, int64(101))
	})
	t.Run("empty", func(t *testing.T) {
		_, ok, err := Latest(strings.NewReader(""), 0)
		testutil.NoError(t, err)
		testutil.False(t, ok)
	})
	t.Run("only the tail window is parsed", func(t *testing.T) {
		old := tokenLine("2026-11-07T10:00:01Z", 100, 1, 0, 200000)
		filler := `{"timestamp":"2026-11-07T10:00:02Z","type":"response_item","payload":{"text":"` + strings.Repeat("x", 4096) + `"}}` + "\n"
		raw := old + strings.Repeat(filler, TailBytes/len(filler)+2)
		_, ok, err := Latest(strings.NewReader(raw), int64(len(raw)))
		testutil.NoError(t, err)
		testutil.False(t, ok)
		raw += tokenLine("2026-11-07T10:05:00Z", 7, 3, 0, 200000)
		r, ok, _ := Latest(strings.NewReader(raw), int64(len(raw)))
		testutil.True(t, ok)
		testutil.Equal(t, r.UsedTokens, int64(10))
	})
}

func TestLatestInFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "rollout-2026-abc.jsonl")
	testutil.NoError(t, os.WriteFile(good, []byte(tokenLine("2026-11-07T10:00:01Z", 10, 5, 0, 1000)), 0o600))
	r, ok, err := LatestInFile(good)
	testutil.NoError(t, err)
	testutil.True(t, ok)
	testutil.Equal(t, r.UsedTokens, int64(15))

	_, ok, err = LatestInFile(filepath.Join(dir, "rollout-missing.jsonl"))
	testutil.NoError(t, err)
	testutil.False(t, ok)

	other := filepath.Join(dir, "auth.json")
	testutil.NoError(t, os.WriteFile(other, []byte("{}"), 0o600))
	_, _, err = LatestInFile(other)
	testutil.True(t, err != nil)

	asDir := filepath.Join(dir, "rollout-dir.jsonl")
	testutil.NoError(t, os.Mkdir(asDir, 0o700))
	_, _, err = LatestInFile(asDir)
	testutil.True(t, err != nil)
}

func makeIndex(t *testing.T, rows ...[4]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state_5.sqlite")
	conn, err := sql.Open("sqlite", p)
	testutil.NoError(t, err)
	defer conn.Close() //nolint:errcheck // test handle
	_, err = conn.Exec(`CREATE TABLE threads (id TEXT, cwd TEXT, rollout_path TEXT, created_at INTEGER, updated_at INTEGER, source TEXT NOT NULL DEFAULT 'cli')`)
	testutil.NoError(t, err)
	for i, r := range rows {
		_, err = conn.Exec(`INSERT INTO threads (id,cwd,rollout_path,created_at,updated_at) VALUES (?,?,?,?,?)`, r[0], r[1], r[2], i, r[3])
		testutil.NoError(t, err)
	}
	return p
}

func TestFindThread(t *testing.T) {
	db := makeIndex(t,
		[4]string{"old", "/w", "/r/rollout-old.jsonl", "1"},
		[4]string{"new", "/w", "/r/rollout-new.jsonl", "5"},
		[4]string{"foreign", "/other", "/r/rollout-foreign.jsonl", "9"},
		[4]string{"blank", "/w", "", "99"},
	)
	t.Run("latest in cwd when no session recorded", func(t *testing.T) {
		th, ok, err := FindThread(db, "/w", "")
		testutil.NoError(t, err)
		testutil.True(t, ok)
		testutil.Equal(t, th.ID, "new")
	})
	t.Run("recorded session pins its own thread", func(t *testing.T) {
		th, ok, _ := FindThread(db, "/w", "old")
		testutil.True(t, ok)
		testutil.Equal(t, th.RolloutPath, "/r/rollout-old.jsonl")
	})
	t.Run("session from another worktree does not match", func(t *testing.T) {
		_, ok, err := FindThread(db, "/w", "foreign")
		testutil.NoError(t, err)
		testutil.False(t, ok)
	})
	t.Run("unknown session", func(t *testing.T) {
		_, ok, _ := FindThread(db, "/w", "gone")
		testutil.False(t, ok)
	})
	t.Run("missing index", func(t *testing.T) {
		_, ok, err := FindThread(filepath.Join(t.TempDir(), "none.sqlite"), "/w", "")
		testutil.NoError(t, err)
		testutil.False(t, ok)
	})
	t.Run("empty cwd rejected", func(t *testing.T) {
		_, _, err := FindThread(db, "", "")
		testutil.True(t, err != nil)
	})
	t.Run("corrupt index errors", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "state_5.sqlite")
		testutil.NoError(t, os.WriteFile(bad, []byte("not a database at all, just text"), 0o600))
		_, _, err := FindThread(bad, "/w", "")
		testutil.True(t, err != nil)
	})
}
