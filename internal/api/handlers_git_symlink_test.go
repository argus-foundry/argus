package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHandleGitDiffFinalSymlinkSwap(t *testing.T) {
	realGit, err := exec.LookPath("git")
	testutil.NoError(t, err)
	for _, directory := range []bool{false, true} {
		name := "file becomes directory"
		if directory {
			name = "directory target replaced"
		}
		t.Run(name, func(t *testing.T) {
			srv, d := testServer(t)
			repo := t.TempDir()
			runGit(t, repo, "init", "-b", "master")
			inside := filepath.Join(repo, "inside")
			if directory {
				testutil.NoError(t, os.Mkdir(inside, 0o700))
				testutil.NoError(t, os.WriteFile(filepath.Join(inside, "null"), []byte("INSIDE\n"), 0o600))
			} else {
				testutil.NoError(t, os.WriteFile(inside, []byte("INSIDE\n"), 0o600))
			}
			outside := t.TempDir()
			testutil.NoError(t, os.WriteFile(filepath.Join(outside, "null"), []byte("OUTSIDE-FINAL-SWAP\n"), 0o600))
			testutil.NoError(t, os.Symlink(inside, filepath.Join(repo, "final")))
			task := &model.Task{Name: "final-link", Status: model.StatusPending, Worktree: repo}
			testutil.NoError(t, d.Add(task))
			bin := t.TempDir()
			shim := `#!/bin/sh
previous=
for arg do
  if [ "$arg" = "--no-index" ] || { [ "$previous" = "--cached" ] && [ "$arg" != "--" ]; }; then
    mv "$INSIDE_DIR" "$SAVED_DIR" || exit 70
    ln -s "$OUTSIDE_DIR" "$INSIDE_DIR" || exit 71
    break
  fi
  previous=$arg
done
exec "$REAL_GIT" "$@"
`
			testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700))
			t.Setenv("REAL_GIT", realGit)
			t.Setenv("INSIDE_DIR", inside)
			t.Setenv("SAVED_DIR", filepath.Join(repo, "saved"))
			t.Setenv("OUTSIDE_DIR", outside)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/diff?path=final", ""))
			testutil.Equal(t, w.Code, http.StatusOK)
			var got struct{ Diff string }
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			testutil.Contains(t, got.Diff, "new file mode 120000")
			testutil.Contains(t, got.Diff, "+"+inside+"\n")
			testutil.False(t, strings.Contains(got.Diff, "OUTSIDE-FINAL-SWAP"))
			target, err := os.Readlink(inside)
			testutil.NoError(t, err)
			testutil.Equal(t, target, outside)
		})
	}
}
