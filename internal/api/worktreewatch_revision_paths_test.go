package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestWtLiteralArrowRevision(t *testing.T) {
	for _, kind := range []string{"untracked", "modified", "quoted rename"} {
		t.Run(kind, func(t *testing.T) {
			repo := wtRepo(t)
			name := "a -> b.txt"
			switch kind {
			case "modified":
				testutil.NoError(t, os.WriteFile(filepath.Join(repo, name), []byte("base\n"), 0o600))
				runGit(t, repo, "add", "--", name)
				runGit(t, repo, "commit", "-qm", "base")
			case "quoted rename":
				name = `dest "quoted" -> name.txt`
				runGit(t, repo, "mv", "--", "f.txt", name)
			}
			path := filepath.Join(repo, name)
			testutil.NoError(t, os.WriteFile(path, []byte("first\n"), 0o600))
			first := gitutil.FetchGitStatus("t", repo, gitutil.Options{})
			testutil.NoError(t, first.Err)
			revision := worktreeRevision(repo, first)
			testutil.NoError(t, os.WriteFile(path, []byte("other\n"), 0o600))
			later := time.Now().Add(time.Second)
			testutil.NoError(t, os.Chtimes(path, later, later))
			second := gitutil.FetchGitStatus("t", repo, gitutil.Options{})
			testutil.NoError(t, second.Err)
			testutil.Equal(t, second.Status, first.Status)
			testutil.Equal(t, second.Diff, first.Diff)
			next := worktreeRevision(repo, second)
			t.Logf("kind=%s status=%q before=%s after=%s", kind, first.Status, revision, next)
			testutil.Equal(t, next != revision, true)
		})
	}
}

func TestWtRevisionPaths(t *testing.T) {
	for _, tc := range []struct {
		name, status, path string
		want               []string
	}{
		{"ordinary", "??", `"a -> b.txt"`, []string{"a -> b.txt"}},
		{"modified", "M", "a -> b.txt", []string{"a -> b.txt"}},
		{"rename", "RM", "old -> new", []string{"old", "new"}},
		{"copy", "C", `"old -> name" -> "new -> name"`, []string{"old -> name", "new -> name"}},
		{"escaped quote", "R", `"old \" -> name" -> "new \\ -> name"`, []string{`old " -> name`, `new \ -> name`}},
		{"rename without pair", "R", `"a -> b.txt"`, []string{"a -> b.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.DeepEqual(t, revisionPaths(tc.status, tc.path), tc.want)
		})
	}
}
