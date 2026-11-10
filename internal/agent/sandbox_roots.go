package agent

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
)

// sandboxTempWriteRoots are the temp directories sandboxProfileBase grants
// file-write on. TestSandboxTempWriteRoots_MatchProfile keeps this list and
// the SBPL text in step.
var sandboxTempWriteRoots = []string{"/private/tmp", "/tmp", "/private/var/folders", "/var/folders"}

// SandboxWriteRoots returns the directories an agent of task can write when
// sandboxed: its worktree, the profile's temp roots and the resolved ExtraWrite
// entries (global + project), each symlink-resolved the way the kernel
// resolves sandbox rules. The unsandboxed daemon confines every path an agent
// hands it to these roots, sandboxed task or not (add-argus-artifact-publish):
// the profile's broad read grant is not used, so the daemon never reads a
// file on an agent's behalf from outside them.
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

// sandboxBuiltinDenyRead are the HOME-relative directories sandboxProfileBase
// denies file-read on. TestSandboxBuiltinDenyRead_MatchProfile keeps this
// list and the SBPL text in step.
var sandboxBuiltinDenyRead = []string{".gnupg", ".aws", ".kube", ".config/gcloud"}

// SandboxDenyReadPaths returns the directories the daemon refuses to read on
// an agent's behalf: the profile's built-in HOME-relative denials plus the
// DenyRead entries of the global config and of every project, expanded and symlink-resolved the way
// GenerateSandboxConfig renders them. The daemon refuses an agent-supplied
// path under any of them even when it lies inside a writable root.
func SandboxDenyReadPaths(task *model.Task, cfg config.Config) []string {
	homeDir, _ := os.UserHomeDir()
	homeDir = evalSymlinksOrKeep(homeDir)
	var out []string
	for _, rel := range sandboxBuiltinDenyRead {
		out = append(out, evalSymlinksOrKeep(filepath.Join(homeDir, rel)))
	}
	// Global deny_read plus EVERY project's, not just the task's: the caller
	// names its task itself, so a spoofed id must not narrow the deny list.
	denies := append([]string{}, cfg.Sandbox.DenyRead...)
	for _, name := range slices.Sorted(maps.Keys(cfg.Projects)) {
		denies = append(denies, cfg.Projects[name].Sandbox.DenyRead...)
	}
	for _, p := range denies {
		p = evalSymlinksOrKeep(expandHomePath(strings.TrimSpace(p), homeDir))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
