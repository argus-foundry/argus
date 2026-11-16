package agent

import (
	"path/filepath"

	"github.com/argus-foundry/argus/internal/browserproc"
	"github.com/argus-foundry/argus/internal/db"
)

// Snapshot before the session's group is signaled; detached Chrome survives
// that signal. Fixed identities also keep a subsequent restart out of scope.
var captureBrowsers = browserproc.Capture
var stopCapturedBrowsers = browserproc.Stop

var cleanupSessionBrowsers = func(worktree string) {
	procs := captureBrowsers(worktree, filepath.Join(db.DataDir(), "worktrees"))
	if len(procs) > 0 {
		go stopCapturedBrowsers(procs, browserproc.Options{})
	}
}

func stopBrowsersFor(worktree string) {
	stopCapturedBrowsers(captureBrowsers(worktree, filepath.Join(db.DataDir(), "worktrees")), browserproc.Options{})
}
