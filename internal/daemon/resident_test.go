package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/api"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/lastexchange"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/notify"
	"github.com/argus-foundry/argus/internal/testutil"
)

// liveHandle is a SessionHandle whose only behavior is Alive.
type liveHandle struct {
	agent.SessionHandle
	alive bool
}

func (h *liveHandle) Alive() bool { return h.alive }

type fakeResRunner struct {
	sess     map[string]*liveHandle
	starts   []startCall
	startErr error
	reattach bool
	pending  map[string]bool
	stopped  []string
}

type startCall struct {
	task   model.Task
	resume bool
}

func (f *fakeResRunner) Get(id string) agent.SessionHandle {
	if h := f.sess[id]; h != nil {
		return h
	}
	return nil
}
func (f *fakeResRunner) StartOrReattach(t *model.Task, _ config.Config, _, _ uint16, resume bool) (agent.SessionHandle, bool, error) {
	f.starts = append(f.starts, startCall{*t, resume})
	if f.startErr != nil {
		return nil, false, f.startErr
	}
	if f.sess == nil {
		f.sess = map[string]*liveHandle{}
	}
	h := &liveHandle{alive: true}
	f.sess[t.ID] = h
	return h, f.reattach, nil
}
func (f *fakeResRunner) Stop(id string) error {
	f.stopped = append(f.stopped, id)
	delete(f.sess, id)
	return nil
}
func (f *fakeResRunner) HasPendingRestart(id string) bool { return f.pending[id] }

type fakeResNotifier struct {
	calls []struct{ task, text, id string }
}

func (f *fakeResNotifier) ReliableNotify(task, text, id string, _ notify.NotifyOpts) func() {
	f.calls = append(f.calls, struct{ task, text, id string }{task, text, id})
	return func() {}
}

type resEnv struct {
	db  *db.DB
	run *fakeResRunner
	cfg config.Config
	m   *residentManager
	now time.Time
}

func newResEnv(t *testing.T) *resEnv {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	oldEnforced := config.ResidentPermissionsEnforced
	config.ResidentPermissionsEnforced = true
	t.Cleanup(func() { config.ResidentPermissionsEnforced = oldEnforced })
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	vault := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Resident.Enabled = true
	cfg.Resident.Name = "Solo"
	cfg.KB.Enabled = true
	cfg.KB.MetisVaultPath = vault
	e := &resEnv{db: d, run: &fakeResRunner{}, cfg: cfg, now: time.Unix(1_800_000_000, 0)}
	e.m = newResidentManager(d, e.run, func() config.Config { return e.cfg })
	e.m.now = func() time.Time { return e.now }
	return e
}

func TestResident_FirstEnableCreatesRowDirAndStarts(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	row, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	if row == nil {
		t.Fatal("resident row not created")
	}
	fi, err := os.Stat(db.ResidentWorkDir())
	testutil.NoError(t, err)
	testutil.Equal(t, fi.IsDir(), true)
	testutil.Equal(t, len(e.run.starts), 1)
	got := e.run.starts[0]
	testutil.Equal(t, got.resume, false)
	testutil.Equal(t, got.task.SessionID != "", true)
	testutil.Equal(t, got.task.Backend, "claude")
	testutil.Equal(t, got.task.Worktree, db.ResidentWorkDir())
}

func TestResident_DisabledStartsNothing(t *testing.T) {
	e := newResEnv(t)
	e.cfg.Resident.Enabled = false
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 0)
	row, _ := e.db.ResidentTask()
	testutil.Equal(t, row == nil, true)
}

func TestResident_PrerequisiteFailuresDoNotStart(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
		reason string
	}{
		{"no name", func(c *config.Config) { c.Resident.Name = "" }, "valid name"},
		{"kb off", func(c *config.Config) { c.KB.Enabled = false }, "kb.enabled"},
		{"vault gone", func(c *config.Config) { c.KB.MetisVaultPath = "/definitely/not/here" }, "metis_vault_path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newResEnv(t)
			tt.mutate(&e.cfg)
			e.m.tick()
			testutil.Equal(t, len(e.run.starts), 0)
			st := e.m.State()
			testutil.Equal(t, st.Enabled, true)
			testutil.Contains(t, st.Reason, tt.reason)
		})
	}
}

func TestResident_NonClaudeDefaultFallsBackOrRefuses(t *testing.T) {
	t.Run("falls back to claude", func(t *testing.T) {
		e := newResEnv(t)
		e.cfg.Defaults.Backend = "codex"
		e.m.tick()
		testutil.Equal(t, len(e.run.starts), 1)
		testutil.Equal(t, e.run.starts[0].task.Backend, "claude")
	})
	t.Run("refuses with reason", func(t *testing.T) {
		e := newResEnv(t)
		e.cfg.Defaults.Backend = "codex"
		e.cfg.Backends = map[string]config.Backend{"codex": e.cfg.Backends["codex"]}
		e.m.tick()
		testutil.Equal(t, len(e.run.starts), 0)
		testutil.Contains(t, e.m.State().Reason, "Claude-family")
	})
}

func TestResident_ModelAccountOverrides(t *testing.T) {
	e := newResEnv(t)
	e.cfg.Resident.Model = "opus"
	e.cfg.Resident.Account = "work"
	e.m.tick()
	testutil.Equal(t, e.run.starts[0].task.Model, "opus")
	testutil.Equal(t, e.run.starts[0].task.Account, "work")
}

func TestResident_AliveSessionNotRestarted(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	e.now = e.now.Add(time.Minute)
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 1)
}

func TestResident_ReattachCountsAsNoFailure(t *testing.T) {
	e := newResEnv(t)
	e.run.reattach = true
	e.m.tick()
	testutil.Equal(t, e.m.failures, 0)
}

func TestResident_BackoffBounded(t *testing.T) {
	e := newResEnv(t)
	e.run.startErr = errors.New("boom")
	var prev time.Duration
	for i := 1; i <= 12; i++ {
		d := residentBackoff(i)
		if d < prev {
			t.Fatalf("backoff decreased at %d: %v < %v", i, d, prev)
		}
		if d > residentBackoffMax {
			t.Fatalf("backoff %v exceeds max", d)
		}
		prev = d
	}
	testutil.Equal(t, residentBackoff(12), residentBackoffMax)
	testutil.Equal(t, residentBackoff(0), time.Duration(0))

	e.m.tick() // attempt 1 fails
	testutil.Equal(t, len(e.run.starts), 1)
	e.m.tick() // within backoff window: no attempt
	testutil.Equal(t, len(e.run.starts), 1)
	e.now = e.now.Add(residentBackoffBase)
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 2)
}

func TestResident_ResumesExistingSessionAfterCrash(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	row, _ := e.db.ResidentTask()
	sid := row.SessionID
	delete(e.run.sess, row.ID)
	e.now = e.now.Add(residentBackoffBase)
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 2)
	testutil.Equal(t, e.run.starts[1].resume, true)
	testutil.Equal(t, e.run.starts[1].task.SessionID, sid)
}

// TestResident_LaunchesNeverReplayRecycleSeed pins the one-shot recycle seed:
// a recycle persists its seed as the row's Prompt, but every launch the ensure
// loop makes — a fresh one with no session ID, or a resume whose transcript may
// be missing and fall back to a fresh launch — carries the orientation.
func TestResident_LaunchesNeverReplayRecycleSeed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sessionID string
		resume    bool
	}{
		{"fresh launch", "", false},
		{"resume after recycle", "recycled-session", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newResEnv(t)
			e.m.tick()
			row, _ := e.db.ResidentTask()
			delete(e.run.sess, row.ID)
			row.SessionID = tc.sessionID
			row.Prompt = "stale recycle seed: relay the old handoff first"
			testutil.NoError(t, e.db.Update(row))
			e.now = e.now.Add(residentBackoffBase)
			e.m.tick()
			last := e.run.starts[len(e.run.starts)-1]
			testutil.Equal(t, last.resume, tc.resume)
			testutil.Equal(t, last.task.Prompt, residentOrientationPrompt(e.cfg))
			testutil.True(t, last.task.SessionID != "")
			stored, _ := e.db.ResidentTask()
			testutil.Equal(t, stored.Prompt, residentOrientationPrompt(e.cfg))
		})
	}
}

func TestResident_SkipsWhileRecycleOrKickInFlight(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	row, _ := e.db.ResidentTask()
	delete(e.run.sess, row.ID)
	e.now = e.now.Add(time.Hour)

	e.m.recycling = func(string) bool { return true }
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 1)
	e.m.recycling = func(string) bool { return false }
	e.run.pending = map[string]bool{row.ID: true}
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 1)
}

func TestResident_StableSessionResetsBackoff(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	testutil.Equal(t, e.m.failures, 1)
	e.now = e.now.Add(time.Second)
	e.m.tick() // observed alive, aliveSince set
	e.now = e.now.Add(residentStableAfter)
	e.m.tick()
	testutil.Equal(t, e.m.failures, 0)
}

func TestResident_DisableStopsButPreservesRow(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	row, _ := e.db.ResidentTask()
	e.cfg.Resident.Enabled = false
	e.m.tick()
	testutil.DeepEqual(t, e.run.stopped, []string{row.ID})
	again, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	testutil.Equal(t, again != nil && again.ID == row.ID, true)
	// Re-enable starts again without a second row.
	e.cfg.Resident.Enabled = true
	e.now = e.now.Add(time.Hour)
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 2)
}

func TestResident_UserStopIsRestartedWhileEnabled(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	row, _ := e.db.ResidentTask()
	delete(e.run.sess, row.ID) // as if stopped via REST/TUI
	e.now = e.now.Add(residentBackoffBase)
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 2)
}

func TestResident_StateUnreadAndViewed(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	e.m.waiting = func(string) bool { return true }
	row, _ := e.db.ResidentTask()
	st := e.m.State()
	testutil.Equal(t, st.Running, true)
	testutil.Equal(t, st.Name, "Solo")
	testutil.Equal(t, st.TaskID, row.ID)
	testutil.Equal(t, st.Unread, false)

	reply := e.now.UTC().Format(db.ResidentTimeFormat)
	testutil.NoError(t, e.db.SetMeta(row.ID, db.ResidentMetaNamespace, db.MetaKeyLastReply, "hello"))
	testutil.NoError(t, e.db.SetMeta(row.ID, db.ResidentMetaNamespace, db.MetaKeyLastReplyAt, reply))
	st = e.m.State()
	testutil.Equal(t, st.Unread, true)
	testutil.Equal(t, st.Waiting, true)
	testutil.Equal(t, st.LastReply, "hello")

	e.now = e.now.Add(time.Second)
	testutil.NoError(t, e.m.MarkViewed())
	testutil.Equal(t, e.m.State().Unread, false)
	testutil.Equal(t, e.m.State().Waiting, true) // viewing does not answer a permission prompt

	// A newer reply re-arms unread.
	e.now = e.now.Add(time.Second)
	testutil.NoError(t, e.db.SetMeta(row.ID, db.ResidentMetaNamespace, db.MetaKeyLastReplyAt, e.now.UTC().Format(db.ResidentTimeFormat)))
	testutil.Equal(t, e.m.State().Unread, true)
}

func TestResident_StateWaitingAndDisabled(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	e.m.waiting = func(string) bool { return true }
	testutil.Equal(t, e.m.State().Waiting, true)
	e.cfg.Resident.Enabled = false
	st := e.m.State()
	testutil.Equal(t, st.Enabled, false)
	testutil.Equal(t, st.Waiting, false)
}

func TestResident_MarkViewedWithoutRow(t *testing.T) {
	e := newResEnv(t)
	if err := e.m.MarkViewed(); err == nil {
		t.Fatal("want error when no resident exists")
	}
}

func TestResident_Send(t *testing.T) {
	e := newResEnv(t)
	n := &fakeResNotifier{}
	e.m.notifier = func() residentNotifier { return n }

	if err := e.m.Send("hi", ""); err == nil {
		t.Fatal("send before the row exists must fail")
	}
	e.m.tick()
	row, _ := e.db.ResidentTask()

	testutil.NoError(t, e.m.Send("hi", "abc"))
	testutil.Equal(t, len(n.calls), 1)
	testutil.Equal(t, n.calls[0].task, row.ID)
	// Argus-originated text is marked so last-exchange extraction skips it.
	testutil.Equal(t, n.calls[0].text, lastexchange.SystemTurnPrefix+"hi")
	testutil.Equal(t, n.calls[0].id, "resident:abc")
	testutil.NoError(t, e.m.Send(lastexchange.SystemTurnPrefix+"marked", "m"))
	testutil.Equal(t, n.calls[1].text, lastexchange.SystemTurnPrefix+"marked") // never double-prefixed
	n.calls = n.calls[:1]

	testutil.NoError(t, e.m.Send("auto-id", ""))
	testutil.Equal(t, n.calls[1].id != "resident:", true)

	if err := e.m.Send("", ""); err == nil {
		t.Fatal("empty text must fail")
	}
	big := make([]byte, residentMaxSendBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := e.m.Send(string(big), ""); err == nil {
		t.Fatal("oversize text must fail")
	}
	e.cfg.Resident.Enabled = false
	if err := e.m.Send("x", ""); err == nil {
		t.Fatal("send while disabled must fail")
	}
	e.cfg.Resident.Enabled = true
	e.m.notifier = func() residentNotifier { return nil }
	if err := e.m.Send("x", ""); err == nil {
		t.Fatal("send without a notifier must fail")
	}
}

func TestResident_RPCRoundTrip(t *testing.T) {
	e := newResEnv(t)
	d := New(e.db)
	d.residentOnce.Do(func() { d.resident = e.m })
	n := &fakeResNotifier{}
	e.m.notifier = func() residentNotifier { return n }
	e.m.tick()
	svc := &RPCService{daemon: d}

	var st ResidentStateResp
	testutil.NoError(t, svc.ResidentState(&Empty{}, &st))
	testutil.Equal(t, st.Running, true)

	var ok StatusResp
	testutil.NoError(t, svc.ResidentMarkViewed(&Empty{}, &ok))
	testutil.Equal(t, ok.OK, true)

	ok = StatusResp{}
	testutil.NoError(t, svc.ResidentSend(&ResidentSendReq{Text: "yo"}, &ok))
	testutil.Equal(t, ok.OK, true)
	testutil.Equal(t, len(n.calls), 1)

	ok = StatusResp{}
	testutil.NoError(t, svc.ResidentSend(&ResidentSendReq{Text: ""}, &ok))
	testutil.Equal(t, ok.OK, false)
	testutil.Equal(t, ok.Error != "", true)
}

// captureSlog swaps the default slog handler for one that records messages.
func captureSlog(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	var msgs []string
	prev := slog.Default()
	slog.SetDefault(slog.New(&recHandler{mu: &mu, msgs: &msgs}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &msgs
}

type recHandler struct {
	mu   *sync.Mutex
	msgs *[]string
}

func (h *recHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	*h.msgs = append(*h.msgs, r.Message)
	h.mu.Unlock()
	return nil
}
func (h *recHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recHandler) WithGroup(string) slog.Handler      { return h }

func TestResident_LogReachesSlog(t *testing.T) {
	msgs := captureSlog(t)
	e := newResEnv(t)
	e.cfg.KB.Enabled = false // prerequisite failure
	e.m.tick()
	found := false
	for _, m := range *msgs {
		if strings.HasPrefix(m, "[resident] not starting:") {
			found = true
		}
	}
	testutil.Equal(t, found, true)
}

func TestResident_StartFailureReasonSurvivesBackoff(t *testing.T) {
	failAgain := func(e *resEnv) {
		t.Helper()
		row, _ := e.db.ResidentTask()
		if row != nil {
			delete(e.run.sess, row.ID)
		}
		e.run.startErr = errors.New("boom")
		e.now = e.now.Add(residentBackoffMax)
		before := len(e.run.starts)
		e.m.tick()
		testutil.Equal(t, len(e.run.starts), before+1)
		testutil.Contains(t, e.m.State().Reason, "start failed: boom")
	}

	t.Run("kept across backoff ticks, cleared on success", func(t *testing.T) {
		e := newResEnv(t)
		e.run.startErr = errors.New("boom")
		e.m.tick()
		testutil.Contains(t, e.m.State().Reason, "start failed: boom")
		testutil.Equal(t, len(e.run.starts), 1)
		e.m.tick() // inside the backoff window: no attempt, reason retained
		testutil.Contains(t, e.m.State().Reason, "start failed: boom")
		testutil.Equal(t, len(e.run.starts), 1)

		e.run.startErr = nil
		e.now = e.now.Add(residentBackoffMax)
		e.m.tick()
		testutil.Equal(t, len(e.run.starts), 2)
		testutil.Equal(t, e.m.State().Reason, "")
	})
	t.Run("cleared once the session is alive", func(t *testing.T) {
		e := newResEnv(t)
		e.run.startErr = errors.New("boom")
		e.m.tick()
		testutil.Contains(t, e.m.State().Reason, "start failed: boom")
		row, _ := e.db.ResidentTask()
		e.run.sess = map[string]*liveHandle{row.ID: {alive: true}}
		e.m.tick()
		testutil.Equal(t, e.m.State().Reason, "")
	})
	t.Run("disable and re-enable drop the stale failure", func(t *testing.T) {
		e := newResEnv(t)
		e.run.startErr = errors.New("boom")
		e.m.tick()
		failAgain(e)

		e.cfg.Resident.Enabled = false
		e.m.tick()
		e.cfg.Resident.Enabled = true
		ready := false
		e.m.mcpReady = func() bool { return ready }
		e.m.tick()
		testutil.Equal(t, e.m.State().Reason, "waiting for the Argus MCP server")

		// Past the prerequisite checks with a restart owned elsewhere, the
		// reason must be empty, not the failure from before the disable.
		ready = true
		e.m.recycling = func(string) bool { return true }
		e.m.tick()
		testutil.Equal(t, e.m.State().Reason, "")
	})
}

func TestResident_PersistentSetupFailureLogsOnce(t *testing.T) {
	msgs := captureSlog(t)
	e := newResEnv(t)
	// A regular file where the work dir must go makes MkdirAll fail.
	testutil.NoError(t, os.MkdirAll(db.DataDir(), 0o755))
	testutil.NoError(t, os.WriteFile(db.ResidentWorkDir(), []byte("x"), 0o600))
	for i := 0; i < 3; i++ {
		e.now = e.now.Add(residentBackoffBase)
		e.m.tick()
	}
	testutil.Equal(t, len(e.run.starts), 0)
	n := 0
	for _, m := range *msgs {
		if strings.HasPrefix(m, "[resident] not starting: create") {
			n++
		}
	}
	testutil.Equal(t, n, 1)
	testutil.Contains(t, e.m.State().Reason, "create")
}

func TestResident_WaitsForMCPServer(t *testing.T) {
	e := newResEnv(t)
	ready := false
	e.m.mcpReady = func() bool { return ready }
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 0)
	testutil.Equal(t, e.m.lastReason, "waiting for the Argus MCP server")
	ready = true
	e.m.tick()
	testutil.Equal(t, len(e.run.starts), 1)
}

func TestResident_DaemonManagerClosures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	database, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	runner := agent.NewRunner(nil)
	d := &Daemon{db: database, sessionCore: newSessionCore(runner, config.DefaultConfig, make(chan struct{}))}
	m := d.residentMgr()
	// A concurrent writer takes d.mu like the daemon's own setters do, so
	// `go test -race` fails if a closure stops locking when it reads these.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			d.mu.Lock()
			d.taskRecycler = nil
			d.apiServer = nil
			d.mcpPort = 0
			d.mu.Unlock()
		}
	}()
	for i := 0; i < 200; i++ {
		_ = m.recycling("r1")
		_ = m.waiting("r1")
		_ = m.mcpReady()
	}
	close(stop)
	wg.Wait()

	testutil.Equal(t, m.mcpReady(), false)
	d.mu.Lock()
	d.mcpPort = 7742
	d.mu.Unlock()
	testutil.Equal(t, m.mcpReady(), true)

	testutil.Equal(t, m.recycling("r1"), false)
	rc := newTaskRecycleRunner(d.db, d.runner, func() config.Config { return config.DefaultConfig() })
	d.mu.Lock()
	d.taskRecycler = rc
	d.mu.Unlock()
	rc.inFlight.Store("r1", struct{}{})
	testutil.Equal(t, m.recycling("r1"), true)
	testutil.Equal(t, m.recycling("other"), false)

	testutil.Equal(t, m.waiting("r1"), false)
	d.mu.Lock()
	d.apiServer = api.New(d.db, d.runner, "tok", nil, nil)
	d.mu.Unlock()
	testutil.Equal(t, m.waiting("r1"), false)
}
