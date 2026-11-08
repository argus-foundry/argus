package daemon

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type censusLine struct {
	Msg  string
	Args map[string]string
}

type censusHarness struct {
	*browserCensus
	browsers []agent.AutomationBrowser
	scanErr  error
	sess     map[int]string
	tasks    map[string]*model.Task
	clock    time.Time
	lines    []censusLine
	sessLook int
}

func newCensusHarness() *censusHarness {
	h := &censusHarness{sess: map[int]string{}, tasks: map[string]*model.Task{}, clock: time.Unix(1_000_000, 0)}
	h.browserCensus = newBrowserCensus(
		func() ([]agent.AutomationBrowser, error) { return h.browsers, h.scanErr },
		func() map[int]string { h.sessLook++; return h.sess },
		func(id string) (*model.Task, error) {
			if t, ok := h.tasks[id]; ok {
				return t, nil
			}
			return nil, errors.New("no such task")
		})
	h.now = func() time.Time { return h.clock }
	h.log = func(msg string, args ...any) {
		l := censusLine{Msg: msg, Args: map[string]string{}}
		for i := 0; i+1 < len(args); i += 2 {
			l.Args[args[i].(string)] = fmt.Sprint(args[i+1])
		}
		h.lines = append(h.lines, l)
	}
	return h
}

func (h *censusHarness) advance(d time.Duration) { h.clock = h.clock.Add(d) }

func (h *censusHarness) take() []censusLine {
	out := h.lines
	h.lines = nil
	return out
}

// codexBrowser: Chrome(100) <- node(30) <- codex agent(20) <- argus(10), no env task ID.
func codexBrowser(ppid int32) agent.AutomationBrowser {
	return agent.AutomationBrowser{PID: 100, PPID: ppid, Start: 4, Ancestors: []agent.BrowserProc{
		{PID: 30, Comm: "node"}, {PID: 20, Comm: "codex"}, {PID: 10, Comm: "argus"},
	}}
}

func TestBrowserCensus_Lifecycle(t *testing.T) {
	h := newCensusHarness()
	h.sess[20] = "t-codex"
	h.tasks["t-codex"] = &model.Task{ID: "t-codex", Status: model.StatusInReview}

	// Present at the first tick: flagged as already running at daemon start,
	// attributed through ancestry (Codex strips ARGUS_TASK_ID).
	h.browsers = []agent.AutomationBrowser{codexBrowser(30)}
	h.tick()
	lines := h.take()
	testutil.Equal(t, len(lines), 1)
	testutil.Equal(t, lines[0].Msg, "browser opened")
	testutil.Equal(t, lines[0].Args["task"], "t-codex")
	testutil.Equal(t, lines[0].Args["task_source"], "ancestry")
	testutil.Equal(t, lines[0].Args["task_status"], "in_review")
	testutil.Equal(t, lines[0].Args["session_live"], "true")
	testutil.Equal(t, lines[0].Args["chain"], "node<codex<argus")
	testutil.Equal(t, lines[0].Args["already_running_at_daemon_start"], "true")

	// Steady state logs nothing.
	h.advance(10 * time.Second)
	h.tick()
	testutil.Equal(t, len(h.take()), 0)

	// Milestone at 10 minutes, once.
	h.advance(10 * time.Minute)
	h.tick()
	h.advance(10 * time.Second)
	h.tick()
	lines = h.take()
	testutil.Equal(t, len(lines), 1)
	testutil.Equal(t, lines[0].Msg, "browser still open")
	testutil.Equal(t, lines[0].Args["launcher_alive"], "true")
	_, hasOrphan := lines[0].Args["orphaned_for"]
	testutil.False(t, hasOrphan)

	// Launcher dies, browser stays.
	h.browsers = []agent.AutomationBrowser{codexBrowser(1)}
	delete(h.sess, 20)
	h.tick()
	lines = h.take()
	testutil.Equal(t, len(lines), 1)
	testutil.Equal(t, lines[0].Msg, "launcher exited but browser still open")
	testutil.Equal(t, lines[0].Args["session_live"], "false")

	// Next milestone (1h) reports the orphaned duration.
	h.advance(time.Hour)
	h.tick()
	lines = h.take()
	testutil.Equal(t, len(lines), 1)
	testutil.Equal(t, lines[0].Args["launcher_alive"], "false")
	testutil.Equal(t, lines[0].Args["orphaned_for"], "1h0m0s")

	// Closed (force-quit).
	h.browsers = nil
	h.advance(time.Minute)
	h.tick()
	lines = h.take()
	testutil.Equal(t, len(lines), 1)
	testutil.Equal(t, lines[0].Msg, "browser closed")
	testutil.Equal(t, lines[0].Args["launcher_alive_at_close"], "false")
	testutil.Equal(t, lines[0].Args["orphaned_for"], "1h1m0s")
	testutil.Equal(t, len(h.entries), 0)
}

func TestBrowserCensus_Attribution(t *testing.T) {
	for _, tc := range []struct {
		name       string
		browser    agent.AutomationBrowser
		sess       map[int]string
		wantTask   string
		wantSource string
	}{
		{"env wins", agent.AutomationBrowser{PID: 1, EnvTaskID: "t-env", Ancestors: []agent.BrowserProc{{PID: 20}}}, map[int]string{20: "t-anc"}, "t-env", "env"},
		{"ancestry", codexBrowser(30), map[int]string{20: "t-anc"}, "t-anc", "ancestry"},
		{"no session ancestor", codexBrowser(30), map[int]string{}, "", ""},
		{"orphaned at first sight, no env", agent.AutomationBrowser{PID: 1, PPID: 1}, map[int]string{20: "t"}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCensusHarness()
			h.sess = tc.sess
			task, source := h.attribute(tc.browser)
			testutil.Equal(t, task, tc.wantTask)
			testutil.Equal(t, source, tc.wantSource)
		})
	}
}

func TestBrowserCensus_OpenedAfterStartAndClosedWithLauncherAlive(t *testing.T) {
	h := newCensusHarness()
	h.tick() // first tick, nothing running
	h.browsers = []agent.AutomationBrowser{{PID: 7, PPID: 30, Start: 1, EnvTaskID: "t1"}}
	h.tasks["t1"] = &model.Task{ID: "t1", Status: model.StatusComplete, Archived: true}
	h.tick()
	lines := h.take()
	testutil.Equal(t, lines[0].Args["already_running_at_daemon_start"], "false")
	testutil.Equal(t, lines[0].Args["task_status"], "complete,archived")
	_, tracked := h.entries[browserID{7, 1}]
	testutil.True(t, tracked)
	h.browsers = nil
	h.tick()
	lines = h.take()
	testutil.Equal(t, lines[0].Msg, "browser closed")
	testutil.Equal(t, lines[0].Args["launcher_alive_at_close"], "true")
	_, hasOrphan := lines[0].Args["orphaned_for"]
	testutil.False(t, hasOrphan)
}

func TestBrowserCensus_OrphanedAtFirstSightAndUnknownTask(t *testing.T) {
	h := newCensusHarness()
	h.browsers = []agent.AutomationBrowser{{PID: 7, PPID: 1, Start: 1, EnvTaskID: "gone"}}
	h.tick()
	lines := h.take()
	testutil.Equal(t, lines[0].Args["task_status"], "unknown")
	testutil.False(t, h.entries[browserID{7, 1}].orphanedAt.IsZero())
	// Stays orphaned: no repeated "launcher exited" line.
	h.tick()
	testutil.Equal(t, len(h.take()), 0)
}

func TestBrowserCensus_UnattributedSkipsLookups(t *testing.T) {
	h := newCensusHarness()
	h.browsers = []agent.AutomationBrowser{{PID: 7, PPID: 1, Start: 1}}
	h.tick()
	lines := h.take()
	testutil.Equal(t, lines[0].Args["task"], "")
	testutil.Equal(t, lines[0].Args["task_status"], "")
	testutil.Equal(t, h.sessLook, 0)
}

func TestBrowserCensus_ScanErrorChangesNothing(t *testing.T) {
	h := newCensusHarness()
	h.browsers = []agent.AutomationBrowser{{PID: 7, PPID: 30, Start: 1}}
	h.tick()
	h.take()
	h.scanErr = errors.New("sysctl failed")
	h.tick()
	h.tick()
	testutil.Equal(t, len(h.take()), 0) // no spurious "closed"
	testutil.Equal(t, h.lastScanErr, "sysctl failed")
	testutil.Equal(t, len(h.entries), 1)
	h.scanErr = nil
	h.tick()
	testutil.Equal(t, h.lastScanErr, "")
}

func TestBrowserCensus_MilestoneSchedule(t *testing.T) {
	h := newCensusHarness()
	testutil.Equal(t, h.nextMilestone(0), 10*time.Minute)
	testutil.Equal(t, h.nextMilestone(1), time.Hour)
	testutil.Equal(t, h.nextMilestone(2), 6*time.Hour)
	testutil.Equal(t, h.nextMilestone(3), 30*time.Hour)
	testutil.Equal(t, h.nextMilestone(4), 54*time.Hour)
}

type fakePIDSession struct {
	agent.SessionHandle
	pid int
}

func (f fakePIDSession) PID() int { return f.pid }

type fakePIDRunner struct {
	agent.SessionRunner
	handles map[string]agent.SessionHandle
}

func (f fakePIDRunner) Running() []string {
	ids := make([]string, 0, len(f.handles))
	for id := range f.handles {
		ids = append(ids, id)
	}
	return ids
}

func (f fakePIDRunner) Get(id string) agent.SessionHandle { return f.handles[id] }

func TestSessionPIDs(t *testing.T) {
	r := fakePIDRunner{handles: map[string]agent.SessionHandle{
		"a":    fakePIDSession{pid: 11},
		"zero": fakePIDSession{pid: 0},
		"nil":  nil,
	}}
	testutil.DeepEqual(t, sessionPIDs(r), map[int]string{11: "a"})
}

func TestCensusLog(t *testing.T) {
	censusLog("browser opened", "pid", int32(5), "task", "t") // must not panic; writes slog + uxlog
}

func TestRunBrowserCensus_TicksAndExitsOnDone(t *testing.T) {
	prev := browserCensusInterval
	browserCensusInterval = 5 * time.Millisecond
	t.Cleanup(func() { browserCensusInterval = prev })

	d, _ := dsServeDaemon(t)
	scans := make(chan struct{}, 1000)
	d.browserScan = func() ([]agent.AutomationBrowser, error) {
		select {
		case scans <- struct{}{}:
		default:
		}
		return []agent.AutomationBrowser{{PID: 7, PPID: 30, Start: 1, EnvTaskID: "missing"}}, nil
	}
	exited := make(chan struct{})
	go func() { d.runBrowserCensus(); close(exited) }()
	for range 2 {
		select {
		case <-scans:
		case <-time.After(3 * time.Second):
			t.Fatal("census never ticked")
		}
	}
	close(d.done)
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("census loop did not exit on done")
	}
}
