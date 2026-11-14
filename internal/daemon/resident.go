package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/backendkind"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/lastexchange"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/notify"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// Resident ensure-running loop tuning (add-resident-agent D3). Vars only so
// tests can shorten them.
var (
	residentTickInterval = 5 * time.Second
	// residentBackoffBase doubles per consecutive failed (re)start up to
	// residentBackoffMax. The first start after a clean slate is immediate.
	residentBackoffBase = 5 * time.Second
	residentBackoffMax  = 5 * time.Minute
	// residentStableAfter is how long a session must stay alive before the
	// failure count resets.
	residentStableAfter = 2 * time.Minute
	// residentPrereqStopAfter is the consecutive-tick count (about 10s) a
	// prerequisite must fail before a running session is stopped.
	residentPrereqStopAfter = 2
)

// residentMaxSendBytes caps one ResidentSend body (matches the task-message cap).
const residentMaxSendBytes = 64 << 10

// residentOrientationPrompt builds the opening prompt of a fresh resident
// session (see resident_memory.go). A var so tests can stub it.
var residentOrientationPrompt = buildResidentOrientation

// residentLog writes a [resident] line to both the uxlog and slog. The daemon
// (and supervisor) never call uxlog.Init, so uxlog.Log alone is a silent no-op
// there; slog reaches the daemon log. Same dual-write as internal/api/bridge.go.
func residentLog(format string, args ...any) {
	uxlog.Log(format, args...)
	slog.Info(fmt.Sprintf(format, args...)) //nolint:gosec // G706: internal diagnostics, not a user-controlled log-forging surface
}

// residentRunner is the slice of agent.SessionRunner the manager uses.
type residentRunner interface {
	Get(taskID string) agent.SessionHandle
	StartOrReattach(task *model.Task, cfg config.Config, rows, cols uint16, resume bool) (agent.SessionHandle, bool, error)
	Stop(taskID string) error
	HasPendingRestart(taskID string) bool
}

// residentNotifier is the slice of *notify.Notifier ResidentSend uses.
type residentNotifier interface {
	ReliableNotify(taskID, text, deliveryID string, opts notify.NotifyOpts) func()
}

// residentManager keeps the singleton resident session running while
// resident.enabled is true, and serves the resident RPCs. Driven by one loop
// (run); State/MarkViewed/Send are safe to call concurrently.
//
// A manual stop of the resident session (REST/TUI stop) is NOT sticky: the loop
// restarts it with the normal backoff, because "enabled" means "kept running".
// The way to stop it is to disable it.
type residentManager struct {
	db        *db.DB
	runner    residentRunner
	cfgFn     func() config.Config
	notifier  func() residentNotifier // late-bound: the notifier is built in Serve
	recycling func(taskID string) bool
	waiting   func(taskID string) bool
	// mcpReady reports whether the daemon's MCP server has bound. nil means
	// ready. The resident's --mcp-config needs the port at spawn, and a session
	// started before it binds never gets the config.
	mcpReady func() bool
	now      func() time.Time
	// commitNotice atomically moves one queued human-action notice into a Hera
	// message (nil = db.DeliverResidentNotice); ringNotice then rings the
	// doorbell for the committed message (nil = store only: tests, no notifier).
	commitNotice func(noticeID, from, to int64, body, tldr string) (*db.HeraMessage, error)
	ringNotice   func(msg *db.HeraMessage)
	// mover restarts the resident in place for an account move (nil = moves
	// and the automatic switch are unavailable: tests, no runner).
	mover residentMover
	// idleTracker adds content-aware idleness to the raw signal for the
	// automatic switch (see sessionIdle).
	idleTracker agent.ContentIdleTracker

	// switchTicks and noneEligible are tick-loop state for the automatic
	// account switch (see maybeAutoSwitch); only tick and MoveAccount touch them.
	switchTicks  int
	noneEligible bool
	noneLoaded   bool // noneEligible was read from task_meta
	// draftFn reports an unsent composer draft (nil = read the rendered screen).
	draftFn   func(agent.SessionHandle) bool
	draftMu   sync.Mutex
	draftView agent.ScreenRenderer

	mu           sync.Mutex
	failures     int
	nextAttempt  time.Time
	aliveSince   time.Time
	lastReason   string // last prerequisite/start failure reason (logged on change)
	loggedReason string
	// startFailure is the last start/persist failure, kept across backoff ticks
	// so the reason (and the log-on-change dedup) is not wiped while waiting
	// for the next attempt. Cleared on a live session, success, or disable.
	startFailure string
	// prereqFails counts consecutive ticks a prerequisite (or the Claude-backend
	// requirement) has failed; a running session is stopped only once it reaches
	// residentPrereqStopAfter, so a transient blip (vault on a briefly
	// unmounted drive) does not kill it.
	prereqFails int
}

func newResidentManager(database *db.DB, runner residentRunner, cfgFn func() config.Config) *residentManager {
	return &residentManager{
		db:        database,
		runner:    runner,
		cfgFn:     cfgFn,
		recycling: func(string) bool { return false },
		waiting:   func(string) bool { return false },
		now:       time.Now,
	}
}

// residentBackend resolves the backend name the resident runs on: the default
// backend when it is Claude-family, else the first Claude-family backend
// (preferring one named "claude"), else an explicit error. The Stop hook and
// task_recycle are Claude-hook based, so no other family is supported.
func residentBackend(cfg config.Config) (string, error) {
	isClaude := func(name string) bool {
		b, ok := cfg.Backends[name]
		return ok && agent.FamilyOf(b.Command).Kind == backendkind.Claude
	}
	if n := cfg.Defaults.Backend; n != "" && isClaude(n) {
		return n, nil
	}
	if isClaude("claude") {
		return "claude", nil
	}
	names := make([]string, 0, len(cfg.Backends))
	for n := range cfg.Backends {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if isClaude(n) {
			return n, nil
		}
	}
	return "", errors.New("the resident requires a Claude-family backend and none is configured")
}

func residentBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	d := residentBackoffBase
	for i := 1; i < failures && d < residentBackoffMax; i++ {
		d *= 2
	}
	if d > residentBackoffMax {
		d = residentBackoffMax
	}
	return d
}

// reason records why the resident is not running, logging only on change.
func (m *residentManager) reason(msg string) {
	m.mu.Lock()
	m.lastReason = msg
	changed := msg != m.loggedReason
	m.loggedReason = msg
	m.mu.Unlock()
	if changed && msg != "" {
		residentLog("[resident] not starting: %s", msg)
	}
}

// prereqFailed handles a failed prerequisite. It always surfaces the reason;
// from the residentPrereqStopAfter-th consecutive failure on it also stops a running
// session, so the resident fails visibly instead of lingering after (say)
// api.enabled is switched off. Stop runs before the reason is set so nothing
// can clear it. The MCP-not-ready wait is a startup state, not a prerequisite,
// and never comes here.
func (m *residentManager) prereqFailed(msg string) {
	m.mu.Lock()
	m.prereqFails++
	stop := m.prereqFails >= residentPrereqStopAfter
	m.mu.Unlock()
	if stop {
		m.stopIfRunning("prerequisite failed: " + msg)
	}
	m.reason(msg)
}

// tick runs one ensure-running pass.
func (m *residentManager) tick() {
	cfg := m.cfgFn()

	if !cfg.Resident.Enabled {
		m.mu.Lock()
		m.prereqFails = 0
		m.mu.Unlock()
		m.reason("")
		m.stopIfRunning("disabled")
		return
	}
	if err := cfg.ResidentPrereq(); err != nil {
		m.prereqFailed(err.Error())
		return
	}
	if m.mcpReady != nil && !m.mcpReady() {
		m.mu.Lock()
		m.prereqFails = 0
		m.mu.Unlock()
		m.reason("waiting for the Argus MCP server")
		return
	}
	backend, err := residentBackend(cfg)
	if err != nil {
		m.prereqFailed(err.Error())
		return
	}
	m.mu.Lock()
	m.prereqFails = 0
	m.mu.Unlock()
	workDir := db.ResidentWorkDir()
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		m.reason(fmt.Sprintf("create %s: %v", workDir, err))
		return
	}
	task, _, err := m.db.EnsureResidentTask(workDir)
	if err != nil {
		m.reason(fmt.Sprintf("resident row: %v", err))
		return
	}
	if _, err := m.db.EnsureResidentOrchestrator(task.ID); err != nil {
		m.reason(fmt.Sprintf("resident orchestrator: %v", err))
		return
	}
	// Every early return above sets its own reason; past them, the reason is
	// the last unresolved start failure (or none). Setting it any earlier would
	// reset the log-on-change dedup each tick and re-log a persistent
	// directory/row failure every 5s.
	m.mu.Lock()
	pending := m.startFailure
	m.mu.Unlock()
	m.reason(pending)

	now := m.now()
	if sess := m.runner.Get(task.ID); sess != nil && sess.Alive() {
		m.mu.Lock()
		if m.aliveSince.IsZero() {
			m.aliveSince = now
		}
		if m.failures > 0 && now.Sub(m.aliveSince) >= residentStableAfter {
			residentLog("[resident] stable for %s, backoff reset", residentStableAfter)
			m.failures = 0
		}
		m.startFailure = ""
		m.mu.Unlock()
		m.reason("")
		// The manual move's own restart has completed: a later restart re-picks.
		if !m.runner.HasPendingRestart(task.ID) {
			m.takeManualPending(task.ID)
		}
		m.maybeAutoSwitch(task, cfg, sess)
		return
	}
	if m.runner.HasPendingRestart(task.ID) || m.recycling(task.ID) {
		return // a kick or task_recycle is mid-flight; it owns the restart
	}

	m.mu.Lock()
	m.aliveSince = time.Time{}
	if now.Before(m.nextAttempt) {
		m.mu.Unlock()
		return
	}
	attempt := m.failures + 1
	m.failures = attempt
	m.nextAttempt = now.Add(residentBackoff(attempt))
	m.mu.Unlock()

	m.start(task, cfg, backend, attempt)
}

// start (re)starts the resident session: it stamps the resolved backend, model,
// account and sandbox on the row, then resumes the prior Claude session unless
// there is none (first start). A recycle never reaches here: it relaunches the
// resident itself with a freshly minted pinned session ID and the resident
// seed (restartSessionWithSeedPrompt), so a later crash restart resumes that.
func (m *residentManager) start(task *model.Task, cfg config.Config, backend string, attempt int) {
	task.Backend = backend
	// Every restart re-picks the best account, except the one a manual move itself
	// caused. A live process (a reattach) is not a restart and never changes
	// account: it is already running on the persisted one.
	own := false
	if m.runner.Get(task.ID) == nil {
		own = m.beginRestart(task)
	}
	if task.SessionID != "" {
		if !residentAccountUsable(cfg, backend, task.Account) {
			if !m.recoverInvalidAccount(task, cfg, backend) {
				return
			}
		} else if !own && m.runner.Get(task.ID) == nil {
			m.repickAtRestart(task, cfg)
		}
	}
	task.Model = cfg.Resident.Model
	task.Account = m.effectiveResidentAccount(task, cfg, backend)
	task.Sandboxed = agent.IsTaskSandboxed(task, cfg)

	m.seedResidentMemory(cfg)

	// A recycle seed is one-shot: the recycle launches with it itself
	// (restartSessionWithSeedPrompt) and never comes through here. Every launch
	// this loop makes carries the current orientation, so a fresh launch after
	// a lost transcript (e.g. resident.account changed) never replays a stale
	// handoff or relay directive. A resume ignores the prompt.
	task.Prompt = residentOrientationPrompt(cfg)
	resume := task.SessionID != ""
	if resume {
		agent.RefreshResumeSessionID(m.db, task)
	} else {
		task.SessionID = model.GenerateSessionID()
	}
	if err := m.db.Update(task); err != nil {
		residentLog("[resident] persist row before start failed: %v", err)
		m.failStart(fmt.Sprintf("persist resident row: %v", err))
		return
	}
	// Launch at the viewer-reported size of the resident tab's OWN pane
	// (db.InitialPTYRoleResident), not Active — Active is whatever view the user
	// is on (a side-paneled agent view, a Hera worker pane), which would commit
	// history at a width the resident tab can never repair. Falls back to the
	// live session's size (a re-attach, or an older TUI that publishes no
	// resident size) and then 24x80.
	var sizer interface{ PTYSize() (int, int) }
	if sess := m.runner.Get(task.ID); sess != nil {
		sizer = sess
	}
	rows, cols, source := resolveSpawnSize(m.db, db.InitialPTYRoleResident, sizer, m.now())
	residentLog("[resident] spawn size source=%s rows=%d cols=%d", source, rows, cols)
	_, reattached, err := m.runner.StartOrReattach(task, cfg, rows, cols, resume)
	if err != nil {
		residentLog("[resident] start failed attempt=%d resume=%t backend=%s: %v", attempt, resume, backend, err)
		m.failStart(fmt.Sprintf("start failed: %v", err))
		return
	}
	if reattached {
		residentLog("[resident] re-attached to running session task=%s", task.ID)
		m.mu.Lock()
		m.failures = 0
		m.startFailure = ""
		m.mu.Unlock()
		m.reason("")
		return
	}
	m.mu.Lock()
	m.startFailure = ""
	m.mu.Unlock()
	m.reason("")
	residentLog("[resident] started task=%s attempt=%d resume=%t backend=%s", task.ID, attempt, resume, backend)
}

// failStart records a start failure so later backoff ticks keep reporting it.
func (m *residentManager) failStart(msg string) {
	m.mu.Lock()
	m.startFailure = msg
	m.mu.Unlock()
	m.reason(msg)
}

func (m *residentManager) stopIfRunning(why string) {
	task, err := m.db.ResidentTask()
	if err != nil || task == nil {
		return
	}
	m.mu.Lock()
	m.failures, m.nextAttempt, m.aliveSince = 0, time.Time{}, time.Time{}
	m.startFailure = ""
	m.mu.Unlock()
	if m.runner.Get(task.ID) == nil && !m.runner.HasPendingRestart(task.ID) {
		return
	}
	if err := m.runner.Stop(task.ID); err != nil {
		residentLog("[resident] stop (%s) failed: %v", why, err)
		return
	}
	residentLog("[resident] stopped task=%s (%s)", task.ID, why)
}

// run drives tick until done closes.
func (m *residentManager) run(done <-chan struct{}) {
	t := time.NewTicker(residentTickInterval)
	defer t.Stop()
	m.tick()
	m.deliverNotices()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			m.tick()
			m.deliverNotices()
		}
	}
}

func parseResidentTime(s string) time.Time {
	t, _ := time.Parse(db.ResidentTimeFormat, s)
	return t
}

func (m *residentManager) metaValues(taskID string) map[string]string {
	out := map[string]string{}
	entries, err := m.db.ListMeta(taskID, db.ResidentMetaNamespace)
	if err != nil {
		return out
	}
	for _, e := range entries {
		out[e.Key] = e.Value
	}
	return out
}

// State reads the resident's daemon-held state. Unread is last_reply_at after
// last_viewed_at; Waiting is the sticky needs-input signal for the resident.
func (m *residentManager) State() ResidentStateResp {
	cfg := m.cfgFn()
	resp := ResidentStateResp{Enabled: cfg.Resident.Enabled, Name: cfg.Resident.Name}
	resp.RunningWorkers = m.runningWorkers()
	if !resp.Enabled {
		return resp
	}
	if err := cfg.ResidentPrereq(); err != nil {
		resp.Reason = err.Error()
	} else {
		m.mu.Lock()
		resp.Reason = m.lastReason
		m.mu.Unlock()
	}
	task, err := m.db.ResidentTask()
	if err != nil || task == nil {
		return resp
	}
	resp.TaskID = task.ID
	if sess := m.runner.Get(task.ID); sess != nil && sess.Alive() {
		resp.Running = true
		resp.Waiting = m.waiting(task.ID)
	}
	meta := m.metaValues(task.ID)
	resp.LastReply = meta[db.MetaKeyLastReply]
	resp.LastReplyAt = meta[db.MetaKeyLastReplyAt]
	resp.ViewedAt = meta[db.MetaKeyLastViewedAt]
	if resp.LastReplyAt != "" {
		resp.Unread = parseResidentTime(resp.LastReplyAt).After(parseResidentTime(resp.ViewedAt))
	}
	return resp
}

// MarkViewed stamps last_viewed_at = now, clearing unread.
func (m *residentManager) MarkViewed() error {
	task, err := m.db.ResidentTask()
	if err != nil {
		return err
	}
	if task == nil {
		return errors.New("no resident agent exists")
	}
	ts := m.now().UTC().Format(db.ResidentTimeFormat)
	if err := m.db.SetMeta(task.ID, db.ResidentMetaNamespace, db.MetaKeyLastViewedAt, ts); err != nil {
		return err
	}
	residentLog("[resident] marked viewed at %s", ts)
	return nil
}

// Send queues text for the resident over the reliable, idle-gated notify bus.
func (m *residentManager) Send(text, deliveryID string) error {
	if text == "" {
		return errors.New("resident send: text must not be empty")
	}
	if len(text) > residentMaxSendBytes {
		return fmt.Errorf("resident send: text exceeds %d bytes", residentMaxSendBytes)
	}
	cfg := m.cfgFn()
	if !cfg.Resident.Enabled {
		return errors.New("resident send: the resident is not enabled")
	}
	task, err := m.db.ResidentTask()
	if err != nil {
		return err
	}
	if task == nil {
		return errors.New("resident send: the resident has not started yet")
	}
	var n residentNotifier
	if m.notifier != nil {
		n = m.notifier()
	}
	if n == nil {
		return errors.New("resident send: delivery service is not available")
	}
	if deliveryID == "" {
		deliveryID = fmt.Sprintf("%d", m.now().UnixNano())
	}
	// Everything sent here is Argus-originated (the context-budget nudge), so
	// mark it: the recycle seed's last-exchange extraction must never mistake
	// it for the user's last message when it is typed without the bridge.
	if !strings.HasPrefix(text, lastexchange.SystemTurnPrefix) {
		text = lastexchange.SystemTurnPrefix + text
	}
	n.ReliableNotify(task.ID, text, "resident:"+deliveryID, notify.NotifyOpts{})
	residentLog("[resident] send queued task=%s delivery=%s bytes=%d", task.ID, deliveryID, len(text))
	return nil
}

// residentMgr returns the daemon's resident manager, building it on first use
// from the runner mounted at that time (UseSupervisorRunner runs before Serve).
func (d *Daemon) residentMgr() *residentManager {
	d.residentOnce.Do(func() {
		m := newResidentManager(d.db, d.runner, d.cfgFn)
		m.notifier = func() residentNotifier {
			if d.notifier == nil {
				return nil
			}
			return d.notifier
		}
		m.recycling = func(id string) bool {
			d.mu.Lock()
			rc := d.taskRecycler
			d.mu.Unlock()
			if rc == nil {
				return false
			}
			_, in := rc.inFlight.Load(id)
			return in
		}
		m.mcpReady = func() bool {
			d.mu.Lock()
			defer d.mu.Unlock()
			return d.mcpPort != 0
		}
		m.ringNotice = func(msg *db.HeraMessage) { d.heraNotifyService().Deliver(msg) }
		m.waiting = func(id string) bool {
			d.mu.Lock()
			srv := d.apiServer
			d.mu.Unlock()
			return srv != nil && srv.ResidentWaiting()
		}
		if mv, ok := d.runner.(residentMover); ok {
			m.mover = mv
		}
		d.resident = m
	})
	return d.resident
}
