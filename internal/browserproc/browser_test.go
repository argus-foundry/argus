package browserproc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

const testRoot = "/test/.argus/worktrees"
const testWT = testRoot + "/project/task"
const chrome = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
const chromeArgs = " --user-data-dir=/tmp/playwright_chromiumdev_profile-abc --remote-debugging-pipe --no-startup-window"

func TestOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, cmd, cwd string
		want           bool
	}{
		{"detached Chrome", chrome + chromeArgs, testWT, true},
		{"nested cwd", chrome + chromeArgs, testWT + "/scripts", true},
		{"personal Chrome", chrome + " --user-data-dir=/personal --remote-debugging-pipe", testWT, false},
		{"personal cwd", chrome + chromeArgs, "/personal", false},
		{"prompt mentioning Chrome", "node agent.js " + chrome + chromeArgs, testWT, false},
		{"cache alone", "/test/.argus/cache/ms-playwright/chrome --user-data-dir=/personal", testWT, false},
		{"managed profile", "/cache/chrome --user-data-dir=" + testWT + "/profile --remote-debugging-pipe", testWT, true},
		{"prefix sibling profile", "/cache/chrome --user-data-dir=" + testWT + "-other/profile --remote-debugging-pipe", testWT, false},
		{"helper is not root", chrome + chromeArgs + " --type=renderer", testWT, false},
		{"project root", chrome + chromeArgs, testRoot + "/project", false},
		{"escaped root", chrome + chromeArgs, testRoot + "-other/project/task", false},
		{"no debugging pipe", chrome + " --user-data-dir=/tmp/playwright_chromiumdev_profile-abc", testWT, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Process{PID: 20, PGID: 20, Command: tc.cmd, CWD: tc.cwd}
			testutil.Equal(t, identify(p, testRoot).Root, tc.want)
		})
	}
}

func TestWorktreeCanonicalOwnership(t *testing.T) {
	root := filepath.Join(t.TempDir(), "worktrees")
	wt := filepath.Join(root, "p", "task")
	testutil.NoError(t, os.MkdirAll(wt, 0700))
	alias := filepath.Join(t.TempDir(), "alias")
	testutil.NoError(t, os.Symlink(root, alias))
	want, err := filepath.EvalSymlinks(wt)
	testutil.NoError(t, err)
	testutil.Equal(t, Worktree(filepath.Join(alias, "p", "task"), root), want)
	outside := t.TempDir()
	escape := filepath.Join(root, "p", "escape")
	testutil.NoError(t, os.Symlink(outside, escape))
	testutil.Equal(t, Worktree(escape, root), "")
	testutil.Equal(t, Worktree("relative", root), "")
}

func TestStopRechecksBeforeEscalating(t *testing.T) {
	for _, tc := range []string{"resumed", "cancelled"} {
		t.Run(tc, func(t *testing.T) {
			fakeInspection(t)
			procs, err := Scan(testRoot)
			testutil.NoError(t, err)
			oldSignal, oldWait := signal, wait
			t.Cleanup(func() { signal = oldSignal; wait = oldWait })
			var sent []syscall.Signal
			signal = func(_ int, sig syscall.Signal) error { sent = append(sent, sig); return nil }
			eligible := true
			cancel := make(chan struct{})
			wait = func(time.Duration, <-chan struct{}) bool {
				if tc == "resumed" {
					eligible = false
				} else {
					close(cancel)
				}
				return true
			}
			Stop(procs, Options{Cancel: cancel, Proceed: func() bool { return eligible }})
			testutil.DeepEqual(t, sent, []syscall.Signal{syscall.SIGTERM, syscall.SIGTERM})
		})
	}
}

func fakeInspection(t *testing.T) (*map[int]Process, *[]string) {
	t.Helper()
	old := output
	t.Cleanup(func() { output = old })
	procs := map[int]Process{
		20: {PID: 20, PPID: 1, PGID: 20, Command: chrome + chromeArgs, CWD: testWT},
		21: {PID: 21, PPID: 20, PGID: 20, Command: "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Helper.app/Contents/MacOS/Google Chrome Helper --type=renderer", CWD: testWT},
		22: {PID: 22, PPID: 1, PGID: 22, Command: chrome + " --user-data-dir=/personal", CWD: "/personal"},
	}
	var calls []string
	output = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "lsof" || name == "readlink" {
			for id, p := range procs {
				if strings.Contains(strings.Join(args, " "), fmt.Sprint(id)) {
					if name == "lsof" {
						return []byte("p20\nn" + p.CWD + "\n"), nil
					}
					return []byte(p.CWD + "\n"), nil
				}
			}
			return nil, errors.New("gone")
		}
		var b strings.Builder
		for id, p := range procs {
			if len(args) == 3 || strings.Join(args, " ") == fmt.Sprintf("-ww -p %d -o pid=,ppid=,pgid=,command=", id) {
				fmt.Fprintf(&b, "%d %d %d %s\n", p.PID, p.PPID, p.PGID, p.Command)
			}
		}
		return []byte(b.String()), nil
	}
	return &procs, &calls
}

func TestScanAndStop(t *testing.T) {
	procs, _ := fakeInspection(t)
	found, err := Scan(testRoot)
	testutil.NoError(t, err)
	testutil.Equal(t, len(found), 2)
	oldSignal, oldWait := signal, wait
	t.Cleanup(func() { signal = oldSignal; wait = oldWait })
	var sent []string
	signal = func(pid int, sig syscall.Signal) error {
		sent = append(sent, fmt.Sprintf("%d:%d", pid, sig))
		return nil
	}
	wait = func(d time.Duration, c <-chan struct{}) bool {
		testutil.Equal(t, d, 5*time.Second)
		// PID reuse after TERM must not cause KILL of personal Chrome.
		p := (*procs)[20]
		p.Command = chrome + " --user-data-dir=/personal"
		(*procs)[20] = p
		// A fresh browser of the same task is not part of the stop snapshot.
		(*procs)[23] = Process{PID: 23, PPID: 1, PGID: 23, Command: chrome + chromeArgs, CWD: testWT}
		return true
	}
	Stop(found, Options{})
	sort.Strings(sent)
	testutil.DeepEqual(t, sent, []string{"20:15", "21:15", "21:9"})
}

func TestCaptureAndScanFailures(t *testing.T) {
	for _, tc := range []string{"capture", "wrong worktree", "cwd error", "oversized ps", "missing cwd", "gone at kill"} {
		t.Run(tc, func(t *testing.T) {
			procs, _ := fakeInspection(t)
			switch tc {
			case "capture":
				testutil.Equal(t, len(Capture(testWT, testRoot)), 2)
			case "wrong worktree":
				testutil.Equal(t, len(Capture(testWT+"-other", testRoot)), 0)
			case "cwd error":
				prior := output
				output = func(name string, args ...string) ([]byte, error) {
					if name != "ps" {
						return nil, errors.New("denied")
					}
					return prior(name, args...)
				}
				p, err := Scan(testRoot)
				testutil.Equal(t, err != nil, true)
				testutil.Equal(t, len(p), 0)
			case "oversized ps":
				output = func(string, ...string) ([]byte, error) { return []byte(strings.Repeat("x", 17<<20)), nil }
				_, err := Scan(testRoot)
				testutil.Equal(t, err != nil, true)
			case "missing cwd":
				prior := output
				output = func(name string, args ...string) ([]byte, error) {
					if name != "ps" {
						return []byte("p20\n"), nil
					}
					return prior(name, args...)
				}
				p, _ := Scan(testRoot)
				testutil.Equal(t, len(p), 0)
			case "gone at kill":
				found, err := Scan(testRoot)
				testutil.NoError(t, err)
				oldSignal, oldWait := signal, wait
				t.Cleanup(func() { signal = oldSignal; wait = oldWait })
				var sent []int
				signal = func(pid int, _ syscall.Signal) error { sent = append(sent, pid); return nil }
				wait = func(time.Duration, <-chan struct{}) bool { delete(*procs, 20); delete(*procs, 21); return true }
				Stop(found, Options{})
				sort.Ints(sent)
				testutil.DeepEqual(t, sent, []int{20, 21})
			}
		})
	}
}

func TestCaptureInspectionUnavailable(t *testing.T) {
	old := output
	t.Cleanup(func() { output = old })
	output = func(string, ...string) ([]byte, error) { return nil, errors.New("denied") }
	testutil.Equal(t, len(Capture(testWT, testRoot)), 0)
}

func TestScanPartialInspection(t *testing.T) {
	for _, failed := range []int{20, 21} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			procs, _ := fakeInspection(t)
			(*procs)[30] = Process{PID: 30, PPID: 1, PGID: 30, Command: chrome + chromeArgs, CWD: testWT}
			prior := output
			denied := errors.New("denied")
			output = func(name string, args ...string) ([]byte, error) {
				if (name == "lsof" && args[3] == fmt.Sprint(failed)) || (name == "readlink" && args[0] == fmt.Sprintf("/proc/%d/cwd", failed)) {
					return nil, denied
				}
				return prior(name, args...)
			}
			found, err := Scan(testRoot)
			var partial *PartialInspectionError
			testutil.Equal(t, errors.As(err, &partial), true)
			testutil.ErrorIs(t, err, denied)
			testutil.DeepEqual(t, partial.PIDs, []int{failed})
			var ids []int
			for _, p := range found {
				ids = append(ids, p.PID)
			}
			sort.Ints(ids)
			want := []int{20, 30}
			if failed == 20 {
				want = []int{30}
			}
			testutil.DeepEqual(t, ids, want)
			testutil.Equal(t, len(Capture(testWT, testRoot)), len(want))
		})
	}
}

func TestDefaultWait(t *testing.T) {
	c := make(chan struct{})
	close(c)
	testutil.Equal(t, wait(time.Hour, c), false)
	testutil.Equal(t, wait(time.Nanosecond, nil), true)
	_, err := output("must-never-run")
	testutil.Equal(t, err != nil, true)
}

func TestStopGuards(t *testing.T) {
	for _, tc := range []string{"changed cwd", "cancelled", "ineligible", "signal error", "inspection error"} {
		t.Run(tc, func(t *testing.T) {
			procs, _ := fakeInspection(t)
			found, err := Scan(testRoot)
			testutil.NoError(t, err)
			oldSignal, oldWait := signal, wait
			t.Cleanup(func() { signal = oldSignal; wait = oldWait })
			var signals int
			signal = func(int, syscall.Signal) error { signals++; return errors.New("denied") }
			wait = func(time.Duration, <-chan struct{}) bool { return tc != "cancelled" }
			o := Options{}
			switch tc {
			case "changed cwd":
				for id, p := range *procs {
					p.CWD = "/personal"
					(*procs)[id] = p
				}
			case "ineligible":
				o.Proceed = func() bool { return false }
			case "inspection error":
				output = func(string, ...string) ([]byte, error) { return nil, errors.New("denied") }
			}
			Stop(found, o)
			want := 0
			if tc == "cancelled" {
				want = 2
			}
			if tc == "signal error" {
				want = 4
			}
			testutil.Equal(t, signals, want)
		})
	}
}

func TestScanErrors(t *testing.T) {
	old := output
	t.Cleanup(func() { output = old })
	output = func(string, ...string) ([]byte, error) { return nil, errors.New("denied") }
	_, err := Scan(testRoot)
	testutil.Equal(t, err != nil, true)
	for _, line := range []string{"garbage", "x 1 2 command", "1 1 1 ", "0 1 1 chrome"} {
		t.Run(line, func(t *testing.T) { _, ok := parse(line); testutil.Equal(t, ok, false) })
	}
}
