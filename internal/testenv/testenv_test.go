package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/drn/argus/internal/claudeenv"
	"github.com/drn/argus/internal/testutil"
)

func TestScrub_RemovesEveryDenylistedName(t *testing.T) {
	for _, name := range Names() {
		t.Setenv(name, "contaminated")
	}
	removed := Scrub()
	testutil.Equal(t, len(removed), len(Names()))
	AssertScrubbed(t)
}

func TestScrub_SpareTestOwnedAndUnrelatedVars(t *testing.T) {
	t.Setenv("ARGUS_TEST_HELPER_KEEP", "1")
	t.Setenv("PATH_LIKE_UNRELATED", "1")
	t.Setenv("CODEX_HOME", "/live/codex")
	Scrub()
	testutil.Equal(t, os.Getenv("ARGUS_TEST_HELPER_KEEP"), "1")
	testutil.Equal(t, os.Getenv("PATH_LIKE_UNRELATED"), "1")
	testutil.Equal(t, os.Getenv("CODEX_HOME"), "")
}

func TestScrub_NoopWhenClean(t *testing.T) {
	Scrub()
	testutil.Equal(t, len(Scrub()), 0)
}

func TestNames_CoversWorkerEnvironment(t *testing.T) {
	names := Names()
	for _, want := range []string{
		"ARGUS_TASK_ID", "ARGUS_ARCHETYPE", "ARGUS_MODEL", "ARGUS_PROFILE",
		"CODEX_HOME", "CODEX_SQLITE_HOME", "CLAUDE_CONFIG_DIR",
		claudeenv.ForcePersistenceVar,
	} {
		if !slices.Contains(names, want) {
			t.Errorf("denylist missing %s", want)
		}
	}
	for _, m := range claudeenv.ParentSessionMarkers {
		if !slices.Contains(names, m) {
			t.Errorf("denylist missing Claude session marker %s", m)
		}
	}
	for _, m := range claudeenv.AuthOverrides {
		if !slices.Contains(names, m) {
			t.Errorf("denylist missing auth override %s", m)
		}
	}
	for _, n := range names {
		if strings.HasPrefix(n, "ARGUS_TEST_") {
			t.Errorf("denylist must not strip test-owned %s", n)
		}
	}
}

// TestEveryTestPackageInstallsScrub walks the repo and fails when a package
// with tests has no TestMain that calls testenv.Main/Scrub, so a new package
// cannot silently reintroduce worker-env contamination.
func TestEveryTestPackageInstallsScrub(t *testing.T) {
	root := filepath.Join("..", "..")
	hasTests := map[string]bool{}
	scrubs := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".claude", "web-workspace", "web-tests", "macos", "vendor", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		hasTests[dir] = true
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			return perr
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "TestMain" || fn.Recv != nil {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "testenv" && (sel.Sel.Name == "Main" || sel.Sel.Name == "Scrub") {
						scrubs[dir] = true
					}
				}
				return true
			})
			// A TestMain that delegates to a same-package helper must still call Scrub there.
			if !scrubs[dir] {
				scrubs[dir] = delegatesToScrubbingHelper(filepath.Dir(path), fn)
			}
		}
		return nil
	})
	testutil.NoError(t, err)
	testutil.Equal(t, len(hasTests) > 20, true)
	for dir := range hasTests {
		// This package IS the implementation; its main_test.go calls Main unqualified.
		if filepath.Base(dir) == "testenv" {
			continue
		}
		if !scrubs[dir] {
			t.Errorf("%s: test package has no TestMain calling testenv.Main/Scrub", filepath.ToSlash(dir))
		}
	}
}

// delegatesToScrubbingHelper accepts TestMain bodies that call a helper func
// in the same directory which itself references testenv.Scrub/Main.
func delegatesToScrubbingHelper(dir string, tm *ast.FuncDecl) bool {
	called := map[string]bool{}
	ast.Inspect(tm, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok {
				called[id.Name] = true
			}
		}
		return true
	})
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !called[fn.Name.Name] {
				continue
			}
			found := false
			ast.Inspect(fn, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "testenv" && (sel.Sel.Name == "Main" || sel.Sel.Name == "Scrub") {
						found = true
					}
				}
				return true
			})
			if found {
				return true
			}
		}
	}
	return false
}
