package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// TestSandboxTempWriteRoots_MatchProfile keeps the confinement roots in step
// with the SBPL base profile: every temp root must be a write grant there.
func TestSandboxTempWriteRoots_MatchProfile(t *testing.T) {
	for _, root := range sandboxTempWriteRoots {
		t.Run(root, func(t *testing.T) {
			testutil.Contains(t, sandboxProfileBase, `(allow file-write* (subpath "`+root+`"))`)
		})
	}
	testutil.Contains(t, sandboxProfileBase, `(allow file-write* (subpath (param "WORKTREE")))`)
}

func TestSandboxWriteRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt := filepath.Join(t.TempDir(), "wt")
	testutil.NoError(t, os.MkdirAll(wt, 0o700))
	extra := filepath.Join(home, "shared")
	testutil.NoError(t, os.MkdirAll(extra, 0o700))
	cfg := config.Config{
		Sandbox: config.SandboxConfig{ExtraWrite: []string{"~/shared", "  "}},
		Projects: map[string]config.Project{
			"p": {Sandbox: config.ProjectSandboxConfig{ExtraWrite: []string{"/opt/argus-proj"}}},
		},
	}
	roots := SandboxWriteRoots(&model.Task{Project: "p", Worktree: wt}, cfg)
	resolved := func(p string) string { r, err := filepath.EvalSymlinks(p); testutil.NoError(t, err); return r }
	want := []string{resolved(wt)}
	for _, r := range sandboxTempWriteRoots {
		want = append(want, evalSymlinksOrKeep(r))
	}
	want = append(want, resolved(extra), "/opt/argus-proj")
	testutil.DeepEqual(t, roots, want)

	t.Run("no worktree", func(t *testing.T) {
		got := SandboxWriteRoots(&model.Task{}, config.Config{})
		testutil.Equal(t, len(got), len(sandboxTempWriteRoots))
		for _, r := range got {
			testutil.False(t, strings.TrimSpace(r) == "")
		}
	})
}

// TestSandboxBuiltinDenyRead_MatchProfile keeps the built-in deny-read list in
// step with the SBPL base profile.
func TestSandboxBuiltinDenyRead_MatchProfile(t *testing.T) {
	for _, rel := range sandboxBuiltinDenyRead {
		t.Run(rel, func(t *testing.T) {
			testutil.Contains(t, sandboxProfileBase, `(deny file-read* (subpath (string-append (param "HOME") "/`+rel+`")))`)
		})
	}
}

func TestSandboxDenyReadPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rhome, err := filepath.EvalSymlinks(home)
	testutil.NoError(t, err)
	cfg := config.Config{
		Sandbox: config.SandboxConfig{DenyRead: []string{"~/private", " "}},
		Projects: map[string]config.Project{
			"p": {Sandbox: config.ProjectSandboxConfig{DenyRead: []string{"/opt/secret"}}},
		},
	}
	got := SandboxDenyReadPaths(&model.Task{Project: "p"}, cfg)
	var want []string
	for _, rel := range sandboxBuiltinDenyRead {
		want = append(want, filepath.Join(rhome, rel))
	}
	want = append(want, filepath.Join(rhome, "private"), "/opt/secret")
	testutil.DeepEqual(t, got, want)
}
