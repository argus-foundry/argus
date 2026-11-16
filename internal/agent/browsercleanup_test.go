package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/browserproc"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSessionBrowserCleanupScope(t *testing.T) {
	for _, scope := range []StopScope{StopTree, StopAgentOnly, StopRerender} {
		t.Run(scope.String(), func(t *testing.T) {
			old := cleanupSessionBrowsers
			t.Cleanup(func() { cleanupSessionBrowsers = old })
			var paths []string
			cleanupSessionBrowsers = func(path string) { paths = append(paths, path) }
			cmd := exec.Command("unused")
			cmd.Dir = t.TempDir()
			done := make(chan struct{})
			close(done)
			s := &Session{Cmd: cmd, done: done}
			testutil.NoError(t, s.StopScoped(scope))
			want := 0
			if scope == StopTree {
				want = 1
			}
			testutil.Equal(t, len(paths), want)
			if want > 0 {
				testutil.Equal(t, paths[0], cmd.Dir)
			}
		})
	}
}

func TestBrowserCleanupWiring(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldCapture, oldStop := captureBrowsers, stopCapturedBrowsers
	t.Cleanup(func() { captureBrowsers = oldCapture; stopCapturedBrowsers = oldStop })
	wt := filepath.Join(db.DataDir(), "worktrees", "project", "task")
	procs := []browserproc.Process{{PID: 20, Root: true, Worktree: wt}}
	captureBrowsers = func(path, root string) []browserproc.Process {
		testutil.Equal(t, path, wt)
		testutil.Equal(t, root, filepath.Join(db.DataDir(), "worktrees"))
		return procs
	}
	stopped := make(chan []browserproc.Process, 2)
	stopCapturedBrowsers = func(got []browserproc.Process, _ browserproc.Options) { stopped <- got }
	cleanupSessionBrowsers(wt)
	select {
	case got := <-stopped:
		testutil.DeepEqual(t, got, procs)
	case <-time.After(5 * time.Second):
		t.Fatal("browser cleanup not dispatched")
	}
	stopBrowsersFor(wt)
	testutil.DeepEqual(t, <-stopped, procs)
	captureBrowsers = func(string, string) []browserproc.Process { return nil }
	cleanupSessionBrowsers(wt)
	testutil.Equal(t, len(stopped), 0)
	done := make(chan struct{})
	close(done)
	testutil.NoError(t, (&Session{done: done}).Stop())
}

func TestWorktreeRemovalStopsBrowsersFirst(t *testing.T) {
	for _, checked := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "checked"}[checked], func(t *testing.T) {
			repo, _ := cleanupRepo(t)
			wt, branch := addCleanupWorktree(t, repo, "browser-removal", false)
			oldCapture, oldStop := captureBrowsers, stopCapturedBrowsers
			t.Cleanup(func() { captureBrowsers = oldCapture; stopCapturedBrowsers = oldStop })
			captureBrowsers = func(path, root string) []browserproc.Process {
				physical, err := filepath.EvalSymlinks(wt)
				testutil.NoError(t, err)
				gotPhysical, err := filepath.EvalSymlinks(path)
				testutil.NoError(t, err)
				testutil.Equal(t, gotPhysical, physical)
				return []browserproc.Process{{PID: 20, Root: true, Worktree: path}}
			}
			var stopped bool
			stopCapturedBrowsers = func(ps []browserproc.Process, _ browserproc.Options) {
				testutil.Equal(t, len(ps), 1)
				_, err := os.Stat(wt)
				testutil.NoError(t, err)
				stopped = true
			}
			if checked {
				testutil.Equal(t, CleanupTaskGit(wt, branch, repo).Worktree, CleanupRemoved)
			} else {
				RemoveWorktree(wt, repo)
			}
			testutil.Equal(t, stopped, true)
			_, err := os.Stat(wt)
			testutil.Equal(t, os.IsNotExist(err), true)
		})
	}
}
