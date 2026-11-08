package agent

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/drn/argus/internal/testutil"
)

// procArgs2 builds a darwin kern.procargs2 buffer: argc, exec path, NUL
// padding, argv, env.
func procArgs2(argv, env []string) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(len(argv))) //nolint:gosec // test fixture: argv is a handful of strings
	buf = append(buf, []byte("/exec/path")...)
	buf = append(buf, 0, 0, 0)
	for _, a := range argv {
		buf = append(buf, []byte(a)...)
		buf = append(buf, 0)
	}
	for _, e := range env {
		buf = append(buf, []byte(e)...)
		buf = append(buf, 0)
	}
	return append(buf, 0, 0)
}

func TestParseProcArgs2(t *testing.T) {
	t.Parallel()
	t.Run("argv and env", func(t *testing.T) {
		argv, env, err := parseProcArgs2(procArgs2(
			[]string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "--remote-debugging-pipe", ""},
			[]string{"HOME=/Users/x", "ARGUS_TASK_ID=42"}))
		testutil.NoError(t, err)
		testutil.DeepEqual(t, argv, []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "--remote-debugging-pipe", ""})
		testutil.DeepEqual(t, env, []string{"HOME=/Users/x", "ARGUS_TASK_ID=42"})
	})
	t.Run("no trailing terminator", func(t *testing.T) {
		buf := procArgs2([]string{"chrome"}, []string{"A=1"})
		argv, env, err := parseProcArgs2(buf[:len(buf)-3]) // drop A=1's NUL and the block end
		testutil.NoError(t, err)
		testutil.DeepEqual(t, argv, []string{"chrome"})
		testutil.DeepEqual(t, env, []string{"A=1"})
	})
	for _, tc := range []struct {
		name string
		buf  []byte
	}{
		{"short", []byte{1, 0}},
		{"no exec terminator", append([]byte{1, 0, 0, 0}, []byte("/exec")...)},
		{"truncated argv", procArgs2([]string{"a"}, nil)[:4+len("/exec/path")+3]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseProcArgs2(tc.buf)
			testutil.Error(t, err)
		})
	}
}

func TestIsChromeMainComm(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		comm string
		want bool
	}{
		{"Google Chrome", true},
		{"Google Chrome fo", true}, // "Google Chrome for Testing", truncated to 16 bytes
		{"Chromium", true},
		{"chrome-headless-", true},
		{"Google Chrome He", false}, // helper processes
		{"node", false},
		{"Superhuman", false},
	} {
		t.Run(tc.comm, func(t *testing.T) {
			testutil.Equal(t, isChromeMainComm(tc.comm), tc.want)
		})
	}
}

func TestClassifyBrowser(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		argv []string
		env  []string
		want browserClass
	}{
		{"user chrome", []string{"Google Chrome"}, []string{"HOME=/h"}, browserClass{}},
		{"automation, no task", []string{"Google Chrome", "--no-first-run", "--remote-debugging-pipe"}, nil, browserClass{automation: true}},
		{"automation, env task", []string{"Google Chrome", "--remote-debugging-pipe"}, []string{"ARGUS_TASK_ID=7"}, browserClass{automation: true, envTaskID: "7"}},
		{"empty task id ignored", []string{"Google Chrome", "--remote-debugging-pipe"}, []string{"ARGUS_TASK_ID="}, browserClass{automation: true}},
		{"flag as argv[0] does not count", []string{"--remote-debugging-pipe"}, nil, browserClass{}},
		{"empty argv", nil, nil, browserClass{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyBrowser(tc.argv, tc.env)
			testutil.Equal(t, got.automation, tc.want.automation)
			testutil.Equal(t, got.envTaskID, tc.want.envTaskID)
		})
	}
}

func TestAncestry(t *testing.T) {
	t.Parallel()
	byPID := map[int32]BrowserProc{
		10: {PID: 10, PPID: 1, Comm: "argus"},
		20: {PID: 20, PPID: 10, Comm: "claude.exe"},
		30: {PID: 30, PPID: 20, Comm: "node"},
		60: {PID: 60, PPID: 61, Comm: "a"}, // cycle
		61: {PID: 61, PPID: 60, Comm: "b"},
	}
	comms := func(ps []BrowserProc) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Comm)
		}
		return out
	}
	testutil.DeepEqual(t, comms(ancestry(BrowserProc{PPID: 30}, byPID)), []string{"node", "claude.exe", "argus"})
	testutil.Equal(t, len(ancestry(BrowserProc{PPID: 1}, byPID)), 0)
	testutil.Equal(t, len(ancestry(BrowserProc{PPID: 777}, byPID)), 0)
	testutil.Equal(t, len(ancestry(BrowserProc{PPID: 60}, byPID)), maxBrowserAncestry)
}

// fakeProcs is a mutable process table plus argv/env per pid for scanner tests.
type fakeProcs struct {
	procs   []BrowserProc
	args    map[int32][2][]string
	snapErr error
}

func (f *fakeProcs) snapshot() ([]BrowserProc, error) { return f.procs, f.snapErr }

func (f *fakeProcs) procArgs(pid int32) ([]string, []string, error) {
	a, ok := f.args[pid]
	if !ok {
		return nil, nil, errors.New("gone")
	}
	return a[0], a[1], nil
}

func (f *fakeProcs) reparent(pid, ppid int32) {
	for i := range f.procs {
		if f.procs[i].PID == pid {
			f.procs[i].PPID = ppid
		}
	}
}

func automationArgs(env ...string) [2][]string {
	return [2][]string{{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "--remote-debugging-pipe"}, env}
}

// argusTree: argus(10) -> codex(20) -> node playwright-mcp(30) -> Chrome(100),
// plus the user's own Chrome (500).
func argusTree() *fakeProcs {
	return &fakeProcs{
		procs: []BrowserProc{
			{PID: 10, PPID: 1, Comm: "argus", Start: 1},
			{PID: 20, PPID: 10, Comm: "codex", Start: 2},
			{PID: 30, PPID: 20, Comm: "node", Start: 3},
			{PID: 100, PPID: 30, PGID: 100, Comm: "Google Chrome", Start: 4},
			{PID: 101, PPID: 100, PGID: 100, Comm: "Google Chrome He", Start: 5},
			{PID: 500, PPID: 1, PGID: 500, Comm: "Google Chrome", Start: 6},
		},
		args: map[int32][2][]string{
			100: automationArgs(),
			500: {{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}, {"HOME=/h"}},
		},
	}
}

func TestBrowserScanner(t *testing.T) {
	t.Parallel()

	t.Run("lists only automation browsers, with ancestry", func(t *testing.T) {
		f := argusTree()
		s := newBrowserScanner(f.snapshot, f.procArgs)
		got, err := s.Scan()
		testutil.NoError(t, err)
		testutil.Equal(t, len(got), 1)
		testutil.Equal(t, got[0].PID, int32(100))
		testutil.Equal(t, got[0].PPID, int32(30))
		testutil.Equal(t, len(got[0].Ancestors), 3)
		testutil.Equal(t, got[0].Ancestors[0].PID, int32(30))
	})

	t.Run("ancestry captured while parented survives orphaning", func(t *testing.T) {
		f := argusTree()
		s := newBrowserScanner(f.snapshot, f.procArgs)
		_, err := s.Scan()
		testutil.NoError(t, err)
		f.reparent(100, 1)
		got, err := s.Scan()
		testutil.NoError(t, err)
		testutil.Equal(t, got[0].PPID, int32(1))
		testutil.Equal(t, len(got[0].Ancestors), 3)
	})

	t.Run("already orphaned at first sight has no ancestry", func(t *testing.T) {
		f := argusTree()
		f.args[100] = automationArgs("ARGUS_TASK_ID=t1")
		f.reparent(100, 1)
		s := newBrowserScanner(f.snapshot, f.procArgs)
		got, err := s.Scan()
		testutil.NoError(t, err)
		testutil.Equal(t, len(got[0].Ancestors), 0)
		testutil.Equal(t, got[0].EnvTaskID, "t1")
	})

	t.Run("argv is read once per process instance", func(t *testing.T) {
		f := argusTree()
		s := newBrowserScanner(f.snapshot, f.procArgs)
		for range 5 {
			_, err := s.Scan()
			testutil.NoError(t, err)
		}
		testutil.Equal(t, s.ArgvReads, 2) // the automation Chrome + the user's Chrome; never helpers
	})

	t.Run("unreadable argv is retried and not cached", func(t *testing.T) {
		f := argusTree()
		delete(f.args, 100)
		s := newBrowserScanner(f.snapshot, f.procArgs)
		got, err := s.Scan()
		testutil.NoError(t, err)
		testutil.Equal(t, len(got), 0)
		f.args[100] = automationArgs()
		got, err = s.Scan()
		testutil.NoError(t, err)
		testutil.Equal(t, len(got), 1)
	})

	t.Run("cache is pruned when the process instance goes away", func(t *testing.T) {
		f := argusTree()
		s := newBrowserScanner(f.snapshot, f.procArgs)
		_, err := s.Scan()
		testutil.NoError(t, err)
		f.procs[3].Start = 99 // pid 100 reused by a new process
		_, err = s.Scan()
		testutil.NoError(t, err)
		_, old := s.cache[procKey{100, 4}]
		testutil.False(t, old)
	})

	t.Run("snapshot error", func(t *testing.T) {
		f := argusTree()
		f.snapErr = errors.New("sysctl failed")
		s := newBrowserScanner(f.snapshot, f.procArgs)
		_, err := s.Scan()
		testutil.Error(t, err)
	})
}
