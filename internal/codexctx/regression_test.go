package codexctx

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/drn/argus/internal/testutil"
)

func TestLatestTotalTokens(t *testing.T) {
	for _, tc := range []struct {
		name, last string
		found      bool
		want       int64
	}{
		{"compaction estimate", `{"input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"total_tokens":200000}`, true, 200000},
		{"empty usage", `{}`, false, 0},
		{"missing total", `{"input_tokens":10,"output_tokens":5}`, false, 0},
		{"null total", `{"total_tokens":null}`, false, 0},
		{"negative total", `{"total_tokens":-1}`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"timestamp":"2026-11-07T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":%s}}}`+"\n", tc.last)
			r, ok, err := Latest(strings.NewReader(raw), int64(len(raw)))
			testutil.NoError(t, err)
			testutil.Equal(t, ok, tc.found)
			testutil.Equal(t, r.UsedTokens, tc.want)
		})
	}
}

func TestFindThreadExcludesSubagents(t *testing.T) {
	db := makeIndex(t, [4]string{"main", "/w", "/r/rollout-main.jsonl", "1"})
	conn, err := sql.Open("sqlite", db)
	testutil.NoError(t, err)
	defer conn.Close() //nolint:errcheck // test handle
	_, err = conn.Exec(`INSERT INTO threads VALUES ('child','/w','/r/rollout-child.jsonl',2,99,'{"subagent":{"thread_spawn":{"parent_thread_id":"main"}}}')`)
	testutil.NoError(t, err)
	th, ok, err := FindThread(db, "/w", "")
	testutil.NoError(t, err)
	testutil.True(t, ok)
	testutil.Equal(t, th.ID, "main")
}

func TestLatestInFileRejectsFIFOWithoutWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-fifo.jsonl")
	testutil.NoError(t, syscall.Mkfifo(path, 0o600))
	done := make(chan error, 1)
	go func() { _, _, err := LatestInFile(path); done <- err }()
	select {
	case err := <-done:
		testutil.True(t, err != nil)
	case <-time.After(200 * time.Millisecond):
		writer, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			testutil.NoError(t, writer.Close())
		}
		readErr := <-done
		testutil.True(t, readErr != nil)
		t.Fatal("rollout FIFO blocked until a writer arrived")
	}
}

func TestLatestRejectsInvalidTotalWithoutLosingSnapshot(t *testing.T) {
	for _, last := range []string{`{}`, `{"total_tokens":null}`, `{"total_tokens":-1}`, `{"total_tokens":"4"}`} {
		t.Run(last, func(t *testing.T) {
			raw := tokenLine("2026-11-07T10:00:01Z", 10, 5, 0, 100) + fmt.Sprintf(`{"timestamp":"2026-11-07T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":%s}}}`+"\n", last)
			reading, ok, err := Latest(strings.NewReader(raw), int64(len(raw)))
			testutil.NoError(t, err)
			testutil.True(t, ok)
			testutil.Equal(t, reading.UsedTokens, int64(15))
			testutil.Equal(t, reading.MeasuredAt, time.Date(2026, 11, 7, 10, 0, 1, 0, time.UTC))
		})
	}
}

func TestFindThreadRejectsPinnedChild(t *testing.T) {
	db := makeIndex(t, [4]string{"child", "/w", "/r/rollout-child.jsonl", "1"})
	conn, err := sql.Open("sqlite", db)
	testutil.NoError(t, err)
	defer conn.Close() //nolint:errcheck // test handle
	_, err = conn.Exec(`UPDATE threads SET source='{"subagent":{}}'`)
	testutil.NoError(t, err)
	_, ok, err := FindThread(db, "/w", "child")
	testutil.NoError(t, err)
	testutil.False(t, ok)
}
