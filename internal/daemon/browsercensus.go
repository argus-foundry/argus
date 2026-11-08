package daemon

import (
	"log/slog"
	"strings"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/uxlog"
)

// Browser census: a log-only diagnostic for "Chrome windows left behind by
// agents". It records each automation browser's lifecycle (Playwright MCP's
// Chrome, launched with --remote-debugging-pipe) to daemon.log under the
// [browsercensus] prefix: which task opened it, whether its launcher died
// first (orphaned) or the browser closed while its launcher lived, and how
// long it stayed open. It never signals anything. A steady-state tick is one
// in-process process-table sysctl (~1ms); task and session lookups happen
// only on lifecycle events.

// browserCensusInterval is the census tick. A var only so the loop test can
// shorten it.
var browserCensusInterval = 10 * time.Second

// browserCensusMilestones are the ages at which a still-open browser is
// logged again, then every browserCensusRepeat after the last one.
var browserCensusMilestones = []time.Duration{10 * time.Minute, time.Hour, 6 * time.Hour}

const browserCensusRepeat = 24 * time.Hour

type browserID struct {
	pid   int32
	start int64
}

type censusEntry struct {
	firstSeen  time.Time
	taskID     string
	taskSource string // "env", "ancestry" or "" (not launched by an argus session)
	chain      string
	launcher   int32 // parent at last tick; 1 once orphaned
	orphanedAt time.Time
	milestone  int // index into the milestone schedule already logged
}

// browserCensus is driven by one loop; not goroutine-safe.
type browserCensus struct {
	scan     func() ([]agent.AutomationBrowser, error)
	sessions func() map[int]string // live agent PID -> task ID
	task     func(id string) (*model.Task, error)
	now      func() time.Time
	log      func(msg string, args ...any)

	entries     map[browserID]*censusEntry
	ticked      bool
	lastScanErr string
}

func newBrowserCensus(scan func() ([]agent.AutomationBrowser, error), sessions func() map[int]string, task func(string) (*model.Task, error)) *browserCensus {
	return &browserCensus{scan: scan, sessions: sessions, task: task, now: time.Now, log: censusLog, entries: map[browserID]*censusEntry{}}
}

func censusLog(msg string, args ...any) {
	slog.Info("[browsercensus] "+msg, args...)
	var b strings.Builder
	b.WriteString("[browsercensus] ")
	b.WriteString(msg)
	for i := 0; i+1 < len(args); i += 2 {
		b.WriteString(" ")
		b.WriteString(args[i].(string))
		b.WriteString("=")
		b.WriteString(slogValue(args[i+1]))
	}
	uxlog.Log("%s", b.String())
}

func slogValue(v any) string { return slog.AnyValue(v).String() }

// attribute names the task that launched b: the first ancestor that is a live
// session's agent process. ARGUS_TASK_ID in the browser's own environment is
// checked first but is not expected in practice: verified live, Playwright's
// Chrome carries no ARGUS_TASK_ID even under a Claude agent (and Codex strips
// it from MCP servers too), so ancestry — captured within one tick of launch,
// while the launcher is alive — is the real signal.
func (c *browserCensus) attribute(b agent.AutomationBrowser) (taskID, source string) {
	if b.EnvTaskID != "" {
		return b.EnvTaskID, "env"
	}
	if len(b.Ancestors) == 0 {
		return "", ""
	}
	sess := c.sessions()
	for _, a := range b.Ancestors {
		if id, ok := sess[int(a.PID)]; ok {
			return id, "ancestry"
		}
	}
	return "", ""
}

func chainOf(ancestors []agent.BrowserProc) string {
	names := make([]string, 0, len(ancestors))
	for _, a := range ancestors {
		names = append(names, a.Comm)
	}
	return strings.Join(names, "<")
}

// taskState describes the task for a log line: its status (plus ",archived")
// and whether its session is live right now.
func (c *browserCensus) taskState(id string) (status string, sessionLive bool) {
	if id == "" {
		return "", false
	}
	t, err := c.task(id)
	if err != nil || t == nil {
		status = "unknown"
	} else {
		status = t.Status.String()
		if t.Archived {
			status += ",archived"
		}
	}
	for _, tid := range c.sessions() {
		if tid == id {
			return status, true
		}
	}
	return status, false
}

func (c *browserCensus) nextMilestone(i int) time.Duration {
	if i < len(browserCensusMilestones) {
		return browserCensusMilestones[i]
	}
	last := browserCensusMilestones[len(browserCensusMilestones)-1]
	return last + time.Duration(i-len(browserCensusMilestones)+1)*browserCensusRepeat
}

func round(d time.Duration) time.Duration { return d.Round(time.Second) }

func (c *browserCensus) tick() {
	browsers, err := c.scan()
	if err != nil {
		if msg := err.Error(); msg != c.lastScanErr {
			c.lastScanErr = msg
			slog.Warn("[browsercensus] process scan failed", "err", err)
			uxlog.Log("[browsercensus] process scan failed: %v", err)
		}
		return
	}
	c.lastScanErr = ""
	now := c.now()
	atStart := !c.ticked
	c.ticked = true
	seen := make(map[browserID]bool, len(browsers))
	for _, b := range browsers {
		id := browserID{b.PID, b.Start}
		seen[id] = true
		e, ok := c.entries[id]
		if !ok {
			e = &censusEntry{firstSeen: now, launcher: b.PPID, chain: chainOf(b.Ancestors)}
			e.taskID, e.taskSource = c.attribute(b)
			if b.PPID == 1 {
				e.orphanedAt = now
			}
			c.entries[id] = e
			status, live := c.taskState(e.taskID)
			c.log("browser opened", "pid", b.PID, "task", e.taskID, "task_source", e.taskSource,
				"task_status", status, "session_live", live, "launcher_pid", b.PPID,
				"chain", e.chain, "already_running_at_daemon_start", atStart)
			continue
		}
		if b.PPID == 1 && e.launcher != 1 {
			e.orphanedAt = now
			status, live := c.taskState(e.taskID)
			c.log("launcher exited but browser still open", "pid", b.PID, "task", e.taskID,
				"task_status", status, "session_live", live, "age", round(now.Sub(e.firstSeen)))
		}
		e.launcher = b.PPID
		if age := now.Sub(e.firstSeen); age >= c.nextMilestone(e.milestone) {
			e.milestone++
			status, live := c.taskState(e.taskID)
			args := []any{"pid", b.PID, "task", e.taskID, "task_status", status, "session_live", live,
				"age", round(age), "launcher_alive", b.PPID != 1}
			if !e.orphanedAt.IsZero() {
				args = append(args, "orphaned_for", round(now.Sub(e.orphanedAt)))
			}
			c.log("browser still open", args...)
		}
	}
	for id, e := range c.entries {
		if seen[id] {
			continue
		}
		delete(c.entries, id)
		status, live := c.taskState(e.taskID)
		args := []any{"pid", id.pid, "task", e.taskID, "task_status", status, "session_live", live,
			"age", round(now.Sub(e.firstSeen)), "launcher_alive_at_close", e.launcher != 1}
		if !e.orphanedAt.IsZero() {
			args = append(args, "orphaned_for", round(now.Sub(e.orphanedAt)))
		}
		c.log("browser closed", args...)
	}
}

// sessionPIDs maps each live session's agent PID to its task ID.
func sessionPIDs(runner agent.SessionProvider) map[int]string {
	out := map[int]string{}
	for _, id := range runner.Running() {
		if h := runner.Get(id); h != nil {
			if pid := h.PID(); pid > 0 {
				out[pid] = id
			}
		}
	}
	return out
}

// runBrowserCensus is the census's d.done-gated loop.
func (d *Daemon) runBrowserCensus() {
	scan := d.browserScan
	if scan == nil {
		scan = agent.NewBrowserScanner().Scan
	}
	c := newBrowserCensus(scan, func() map[int]string { return sessionPIDs(d.runner) }, d.db.Get)
	ticker := time.NewTicker(browserCensusInterval)
	defer ticker.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			c.tick()
		}
	}
}
