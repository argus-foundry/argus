package gitutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func commitFile(t *testing.T, dir, name, msg string) {
	t.Helper()
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644))
	gitRun(t, dir, "add", name)
	gitRun(t, dir, "commit", "-m", msg)
}

// stackedRepo builds master <- parent (parent.txt) <- child (child.txt) with an
// unrelated upstream tracking ref pointing at master's initial commit.
func stackedRepo(t *testing.T) (dir, parentSHA string) {
	t.Helper()
	dir = initRepo(t, t.TempDir())
	gitRun(t, dir, "update-ref", "refs/remotes/origin/master", headSHA(t, dir))
	gitRun(t, dir, "checkout", "-b", "parent")
	commitFile(t, dir, "parent.txt", "parent work")
	parentSHA = headSHA(t, dir)
	gitRun(t, dir, "checkout", "-b", "child")
	commitFile(t, dir, "child.txt", "child work")
	return dir, parentSHA
}

func TestResolveComparison(t *testing.T) {
	t.Run("origin-only namespaced requested base", func(t *testing.T) {
		dir, parentSHA := stackedRepo(t)
		gitRun(t, dir, "update-ref", "refs/remotes/origin/argus/orch/parent", parentSHA)
		gitRun(t, dir, "branch", "-D", "parent")
		cmp := resolveComparison(dir, Options{BaseBranch: "argus/orch/parent"})
		testutil.Equal(t, cmp.RequestedBase, "argus/orch/parent")
		testutil.Equal(t, cmp.ResolvedBase, "origin/argus/orch/parent")
		testutil.Equal(t, cmp.MergeBase, parentSHA)
		testutil.Nil(t, cmp.FallbackReason)
	})

	t.Run("requested base wins over upstream and default", func(t *testing.T) {
		dir, parentSHA := stackedRepo(t)
		trackOrigin(t, dir)
		cmp := resolveComparison(dir, Options{BaseBranch: "parent"})
		testutil.Equal(t, cmp.RequestedBase, "parent")
		testutil.Equal(t, cmp.ResolvedBase, "parent")
		testutil.Equal(t, cmp.MergeBase, parentSHA)
		testutil.Nil(t, cmp.FallbackReason)
	})

	t.Run("no requested base uses upstream", func(t *testing.T) {
		dir, _ := stackedRepo(t)
		trackOrigin(t, dir)
		cmp := resolveComparison(dir, Options{})
		testutil.Equal(t, cmp.ResolvedBase, "origin/master")
		testutil.Nil(t, cmp.FallbackReason)
	})

	t.Run("missing base falls back with reason", func(t *testing.T) {
		dir, _ := stackedRepo(t)
		cmp := resolveComparison(dir, Options{BaseBranch: "gone"})
		testutil.Equal(t, cmp.RequestedBase, "gone")
		testutil.Equal(t, cmp.ResolvedBase, "origin/master")
		if cmp.MergeBase == "" || cmp.FallbackReason == nil || *cmp.FallbackReason == "" {
			t.Fatalf("expected fallback with reason, got %+v", cmp)
		}
	})

	t.Run("option-looking base is rejected as ref", func(t *testing.T) {
		dir, _ := stackedRepo(t)
		cmp := resolveComparison(dir, Options{BaseBranch: "--output=/tmp/x"})
		if cmp.FallbackReason == nil {
			t.Fatal("expected fallback reason")
		}
		testutil.Equal(t, cmp.ResolvedBase, "origin/master")
	})
}

func TestStatusAndDiffShareBase(t *testing.T) {
	dir, parentSHA := stackedRepo(t)
	trackOrigin(t, dir)
	testutil.NoError(t, os.WriteFile(filepath.Join(dir, "local.txt"), []byte("wip\n"), 0o644))
	opts := Options{BaseBranch: "parent"}

	st := FetchGitStatus("t", dir, opts)
	testutil.NoError(t, st.Err)
	testutil.Contains(t, st.BranchFiles, "child.txt")
	if got := st.BranchFiles; contains(got, "parent.txt") {
		t.Errorf("parent change leaked into branch files: %q", got)
	}
	testutil.Contains(t, st.BranchDiff, "child.txt")
	testutil.Contains(t, st.Status, "local.txt")
	testutil.Equal(t, st.Comparison.MergeBase, parentSHA)

	df := FetchFileDiff("t", dir, "child.txt", opts)
	testutil.NoError(t, df.Err)
	testutil.Contains(t, df.Diff, "child.txt")
	testutil.Contains(t, df.Diff, "new file mode")
	testutil.Contains(t, df.Diff, "@@")
	testutil.Contains(t, df.Diff, "+child.txt\n")
	testutil.Equal(t, df.Comparison.MergeBase, parentSHA)
	testutil.Equal(t, df.Comparison.ResolvedBase, st.Comparison.ResolvedBase)

	pf := FetchFileDiff("t", dir, "parent.txt", opts)
	testutil.Equal(t, pf.Diff, "")

	lf := FetchFileDiff("t", dir, "local.txt", opts)
	testutil.Contains(t, lf.Diff, "local.txt")
	testutil.Contains(t, lf.Diff, "new file mode")
	testutil.Contains(t, lf.Diff, "@@")
	testutil.Contains(t, lf.Diff, "+wip\n")

	unscoped := FetchGitStatus("t", dir, Options{})
	testutil.Contains(t, unscoped.BranchFiles, "parent.txt")
}

func TestFetchGitStatusErr(t *testing.T) {
	st := FetchGitStatus("t", t.TempDir(), Options{})
	if st.Err == nil {
		t.Fatal("expected Err for non-repo worktree")
	}
}

func contains(s, sub string) bool { return len(sub) > 0 && strings.Contains(s, sub) }

func trackOrigin(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "config", "remote.origin.url", dir)
	gitRun(t, dir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	gitRun(t, dir, "config", "branch.child.remote", "origin")
	gitRun(t, dir, "config", "branch.child.merge", "refs/heads/master")
}
