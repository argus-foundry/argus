package agent

import (
	"os"
	"strings"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
)

// sandboxTempWriteRoots are the temp directories sandboxProfileBase grants
// file-write on. TestSandboxTempWriteRoots_MatchProfile keeps this list and
// the SBPL text in step.
var sandboxTempWriteRoots = []string{"/private/tmp", "/tmp", "/private/var/folders", "/var/folders"}

// SandboxWriteRoots returns the directories a sandboxed agent of task can
// write: its worktree, the profile's temp roots and the resolved ExtraWrite
// entries (global + project), each symlink-resolved the way the kernel
// resolves sandbox rules. The unsandboxed daemon uses it to confine a path an
// agent hands it to files that agent could have produced itself
// (add-argus-artifact-publish): the profile's broad read grant is not used,
// so the daemon never reads a file on an agent's behalf from outside these.
func SandboxWriteRoots(task *model.Task, cfg config.Config) []string {
	var roots []string
	if task.Worktree != "" {
		roots = append(roots, evalSymlinksOrKeep(task.Worktree))
	}
	for _, r := range sandboxTempWriteRoots {
		roots = append(roots, evalSymlinksOrKeep(r))
	}
	homeDir, _ := os.UserHomeDir()
	homeDir = evalSymlinksOrKeep(homeDir)
	for _, p := range ResolveSandboxConfig(task, cfg).ExtraWrite {
		p = evalSymlinksOrKeep(expandHomePath(strings.TrimSpace(p), homeDir))
		if p != "" {
			roots = append(roots, p)
		}
	}
	return roots
}
