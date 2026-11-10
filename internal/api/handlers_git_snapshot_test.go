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

func TestHandleGitDiffSymlinkSwap(t *testing.T) {
	realGit, err := exec.LookPath("git")
	testutil.NoError(t, err)
	for _, committed := range []bool{false, true} {
		name := "unborn"
		if committed {
			name = "committed"
		}
		t.Run(name, func(t *testing.T) {
			srv, d := testServer(t)
			repo := t.TempDir()
			runGit(t, repo, "init", "-b", "master")
			if committed {
				runGit(t, repo, "config", "user.email", "t@t")
				runGit(t, repo, "config", "user.name", "t")
				runGit(t, repo, "config", "commit.gpgsign", "false")
				testutil.NoError(t, os.WriteFile(filepath.Join(repo, "base"), []byte("base\n"), 0o600))
				runGit(t, repo, "add", "base")
				runGit(t, repo, "commit", "-m", "base")
			}
			inside := filepath.Join(repo, "inside")
			outside := t.TempDir()
			testutil.NoError(t, os.Mkdir(inside, 0o700))
			testutil.NoError(t, os.WriteFile(filepath.Join(inside, "secret"), []byte("INSIDE-SNAPSHOT\n"), 0o600))
			testutil.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("OUTSIDE-SWAPPED\n"), 0o600))
			link := filepath.Join(repo, "switch")
			testutil.NoError(t, os.Symlink(inside, link))
			task := &model.Task{Name: "swap", Status: model.StatusPending, Worktree: repo}
			testutil.NoError(t, d.Add(task))

			bin := t.TempDir()
			shim := `#!/bin/sh
previous=
for arg do
  if [ "$previous" = "--cached" ] && [ "$arg" != "--" ]; then
    rm "$SWAP_LINK" || exit 70
    ln -s "$OUTSIDE_DIR" "$SWAP_LINK" || exit 71
    break
  fi
  previous=$arg
done
exec "$REAL_GIT" "$@"
`
			testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700))
			t.Setenv("REAL_GIT", realGit)
			t.Setenv("SWAP_LINK", link)
			t.Setenv("OUTSIDE_DIR", outside)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			w := httptest.NewRecorder()
			h := authMiddleware(srv.token, d, nil, srv.routes())
			h.ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/diff?path=switch/secret", ""))
			testutil.Equal(t, w.Code, http.StatusOK)
			var got struct{ Diff string }
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			testutil.Contains(t, got.Diff, "+INSIDE-SNAPSHOT\n")
			testutil.False(t, strings.Contains(got.Diff, "OUTSIDE-SWAPPED"))
			target, err := os.Readlink(link)
			testutil.NoError(t, err)
			testutil.Equal(t, target, outside)
		})
	}
}
