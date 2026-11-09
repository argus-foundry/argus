package api

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func TestTaskUsage_ReplacementWithoutMeasurement(t *testing.T) {
	for _, mode := range []string{"no usage", "missing rollout", "unreadable rollout"} {
		t.Run(mode, func(t *testing.T) {
			e := newCodexCtxEnv(t, "old")
			e.addThread(t, "old", "/w/cx", 1, twoTurns())
			getUsage(t, e.s, e.task.ID, "")
			p := e.addThread(t, "new", "/w/cx", 2, usageStart("2026-11-07T11:00:00Z", "new-turn"))
			if mode != "no usage" {
				testutil.NoError(t, os.Remove(p))
				if mode == "unreadable rollout" {
					testutil.NoError(t, syscall.Mkfifo(p, 0o600))
				}
			}
			e.task.SessionID = "new"
			testutil.NoError(t, e.s.db.Update(e.task))
			code, body := getUsage(t, e.s, e.task.ID, "")
			testutil.Equal(t, code, 200)
			eqAny(t, body["session_id"], "new")
			testutil.Nil(t, body["totals"])
		})
	}
}

func TestTaskUsage_RolloutIdentity(t *testing.T) {
	for _, kind := range []string{"wrong session", "wrong worktree", "missing metadata"} {
		t.Run(kind, func(t *testing.T) {
			e := newCodexCtxEnv(t, "own")
			p := e.addThread(t, "own", "/w/cx", 1, "")
			id, cwd := "own", "/w/cx"
			if kind == "wrong session" {
				id = "foreign"
			}
			if kind == "wrong worktree" {
				cwd = "/elsewhere"
			}
			meta := codexSessionMeta(id, cwd, "2026-11-07T10:00:00Z")
			if kind == "missing metadata" {
				meta = ""
			}
			testutil.NoError(t, os.WriteFile(p, []byte(meta+twoTurns()), 0o600))
			_, ctx := e.get(t)
			testutil.Nil(t, ctx["context"])
			code, body := getUsage(t, e.s, e.task.ID, "")
			testutil.Equal(t, code, 200)
			testutil.Nil(t, body["totals"])
			stored, err := e.s.db.TaskUsageSession(e.task.ID, "own")
			testutil.NoError(t, err)
			testutil.Nil(t, stored)
		})
	}
}

func TestTaskUsage_UncapturedFreshLaunch(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "old", "/w/cx", 1, twoTurns())
	e.run(false)
	_, ctx := e.get(t)
	testutil.Nil(t, ctx["context"])
	code, body := getUsage(t, e.s, e.task.ID, "")
	testutil.Equal(t, code, 200)
	testutil.Nil(t, body["totals"])
}

func TestTaskUsage_IngestOrderPublishesOnce(t *testing.T) {
	for _, getFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(getFirst), func(t *testing.T) {
			e := newCodexCtxEnv(t, "thr1")
			p := e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
			e.run(false)
			sink := &ctxSink{}
			prev := events.SetSink(sink)
			t.Cleanup(func() { events.SetSink(prev) })
			state := newIdleWatcherState()
			ingest := func() {
				if getFirst {
					getUsage(t, e.s, e.task.ID, "")
				}
				e.s.idleWatcherTick(state)
				getUsage(t, e.s, e.task.ID, "")
				e.s.idleWatcherTick(state)
			}
			ingest()
			evs := sink.ofType(model.EventTypeTaskUsageChanged)
			testutil.Equal(t, len(evs), 1)
			testutil.Equal(t, string(evs[0].Payload), `{"measured_at":"2026-11-07T10:01:01Z","session_id":"thr1","turn_id":"turn2"}`)
			// A newly started turn changes the snapshot before session counters advance.
			raw := codexSessionMeta("thr1", "/w/cx", "2026-11-07T10:00:00Z") + twoTurns() + usageStart("2026-11-07T10:02:00Z", "turn3")
			testutil.NoError(t, os.WriteFile(p, []byte(raw), 0o600))
			ingest()
			evs = sink.ofType(model.EventTypeTaskUsageChanged)
			testutil.Equal(t, len(evs), 2)
			testutil.Equal(t, string(evs[1].Payload), `{"measured_at":"2026-11-07T10:01:01Z","session_id":"thr1","turn_id":"turn3"}`)
			raw += usageCount("2026-11-07T10:02:01Z", 1800, 900, 160, 100, 0, 10)
			testutil.NoError(t, os.WriteFile(p, []byte(raw), 0o600))
			ingest()
			evs = sink.ofType(model.EventTypeTaskUsageChanged)
			testutil.Equal(t, len(evs), 3)
			testutil.Equal(t, string(evs[2].Payload), `{"measured_at":"2026-11-07T10:02:01Z","session_id":"thr1","turn_id":"turn3"}`)
		})
	}
}

func TestTaskUsage_UncapturedLaunchHidesStoredUsage(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			e := newCodexCtxEnv(t, "")
			e.addThread(t, "old", "/w/cx", 1, twoTurns())
			getUsage(t, e.s, e.task.ID, "")
			e.run(pending)
			code, body := getUsage(t, e.s, e.task.ID, "")
			testutil.Equal(t, code, 200)
			testutil.Nil(t, body["totals"])
			testutil.Equal(t, len(turnIDs(body)), 0)
		})
	}
}

func TestTaskUsage_UnsupportedUnknownSession(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	code, _ := getUsage(t, s, task.ID, "?session_id=unknown")
	testutil.Equal(t, code, 404)
}

func TestTaskUsage_LimitCap(t *testing.T) {
	e := newCodexCtxEnv(t, "thr1")
	var raw strings.Builder
	for i := 0; i < 205; i++ {
		at := time.Date(2026, 11, 7, 10, 0, i, 0, time.UTC).Format(time.RFC3339Nano)
		raw.WriteString(usageStart(at, fmt.Sprintf("turn%03d", i)) + usageCount(at, int64(i+1), 0, 0, 1, 0, 0))
	}
	e.addThread(t, "thr1", "/w/cx", 1, raw.String())
	code, body := getUsage(t, e.s, e.task.ID, "?limit=100000")
	testutil.Equal(t, code, 200)
	testutil.Equal(t, len(turnIDs(body)), 200)
	testutil.True(t, body["next_cursor"] != nil)
}

func TestTaskUsage_DaemonLogging(t *testing.T) {
	e := newCodexCtxEnv(t, "thr1")
	p := e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
	testutil.NoError(t, os.Remove(p))
	testutil.NoError(t, syscall.Mkfifo(p, 0o600))
	var captured usageLogBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	code, body := getUsage(t, e.s, e.task.ID, "")
	testutil.Equal(t, code, 200)
	testutil.Nil(t, body["totals"])
	testutil.Contains(t, captured.String(), "[usage] read failed")
}

func TestTaskUsage_UnsupportedKeepsStoredCountersUnavailable(t *testing.T) {
	e := newCodexCtxEnv(t, "thr1")
	e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
	getUsage(t, e.s, e.task.ID, "")
	testutil.NoError(t, e.s.db.SetBackend("codex-test", config.Backend{Command: "claude"}))
	for _, query := range []string{"", "?session_id=thr1"} {
		t.Run(query, func(t *testing.T) {
			code, body := getUsage(t, e.s, e.task.ID, query)
			testutil.Equal(t, code, 200)
			testutil.Nil(t, body["totals"])
			testutil.Equal(t, len(turnIDs(body)), 0)
			eqAny(t, body["provider"], "anthropic")
		})
	}
}

func TestTaskUsage_UnpinnedCurrentBeforeMeasurement(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			e := newCodexCtxEnv(t, "")
			e.addThread(t, "old", "/w/cx", 1, twoTurns())
			getUsage(t, e.s, e.task.ID, "")
			if live {
				e.run(false)
			}
			p := e.addThread(t, "new", "/w/cx", 2, "")
			at := time.Now().UTC().Format(time.RFC3339Nano)
			testutil.NoError(t, os.WriteFile(p, []byte(codexSessionMeta("new", "/w/cx", at)+usageStart(at, "new-turn")), 0o600))
			code, body := getUsage(t, e.s, e.task.ID, "")
			testutil.Equal(t, code, 200)
			eqAny(t, body["session_id"], "new")
			testutil.Nil(t, body["totals"])
			testutil.Nil(t, body["measured_at"])
			testutil.Equal(t, len(turnIDs(body)), 0)
			stored, err := e.s.db.TaskUsageSession(e.task.ID, "new")
			testutil.NoError(t, err)
			testutil.Nil(t, stored)
			_, old := getUsage(t, e.s, e.task.ID, "?session_id=old")
			eqAny(t, old["totals"].(map[string]any)["input_tokens"], float64(1700))
		})
	}
}

func TestTaskUsage_WatcherWithoutGET(t *testing.T) {
	e := newCodexCtxEnv(t, "thr1")
	e.addThread(t, "thr1", "/w/cx", 1, twoTurns())
	e.run(false)
	sink := &ctxSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	state := newIdleWatcherState()
	e.s.idleWatcherTick(state)
	e.s.idleWatcherTick(state)
	evs := sink.ofType(model.EventTypeTaskUsageChanged)
	testutil.Equal(t, len(evs), 1)
	stored, err := e.s.db.TaskUsageSession(e.task.ID, "thr1")
	testutil.NoError(t, err)
	testutil.True(t, stored != nil)
}

func TestTaskUsage_DaemonLogsOmitPrivatePaths(t *testing.T) {
	e := newCodexCtxEnv(t, "thr1")
	privateDir := filepath.Join(e.dir, "PRIVATE_PATH_SENTINEL")
	testutil.NoError(t, os.Mkdir(privateDir, 0o700))
	testutil.NoError(t, os.Chmod(privateDir, 0))
	t.Cleanup(func() { testutil.NoError(t, os.Chmod(privateDir, 0o700)) }) //nolint:gosec // Restore directory traversal for fixture cleanup.
	t.Setenv("CODEX_SQLITE_HOME", privateDir)
	var captured usageLogBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&captured, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	code, body := getUsage(t, e.s, e.task.ID, "")
	testutil.Equal(t, code, 200)
	testutil.Nil(t, body["totals"])
	testutil.Contains(t, captured.String(), "[usage] read failed")
	testutil.Contains(t, captured.String(), `"kind":"permission_denied"`)
	testutil.False(t, strings.Contains(captured.String(), "PRIVATE_PATH_SENTINEL"))
	testutil.False(t, strings.Contains(captured.String(), e.dir))
}

func TestUsageErrorKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"permission", &os.PathError{Op: "lstat", Path: "PRIVATE_PATH_SENTINEL", Err: os.ErrPermission}, "permission_denied"},
		{"missing", fmt.Errorf("wrapped: %w", os.ErrNotExist), "not_found"},
		{"closed", os.ErrClosed, "closed"},
		{"other", errors.New("PRIVATE_PATH_SENTINEL"), "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) { testutil.Equal(t, usageErrorKind(tc.err), tc.want) })
	}
}

func TestTaskUsage_UnmeasuredPreviousLaunchIsUnavailable(t *testing.T) {
	e := newCodexCtxEnv(t, "")
	e.addThread(t, "old", "/w/cx", 1, usageStart("2026-11-07T10:00:00Z", "old-turn"))
	e.run(false)
	code, body := getUsage(t, e.s, e.task.ID, "")
	testutil.Equal(t, code, 200)
	eqAny(t, body["session_id"], "")
	testutil.Nil(t, body["totals"])
}

type usageLogBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *usageLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *usageLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
