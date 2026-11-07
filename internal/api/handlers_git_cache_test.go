package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHandleGitDiffTextconvCacheIsolation(t *testing.T) {
	for _, driver := range []string{"custom", "a=b", "custom.one"} {
		t.Run(driver, func(t *testing.T) {
			t.Setenv("GIT_CONFIG_COUNT", "2")
			t.Setenv("GIT_CONFIG_KEY_0", "diff.srcprefix")
			t.Setenv("GIT_CONFIG_VALUE_0", "inherited/")
			t.Setenv("GIT_CONFIG_KEY_1", "diff."+driver+".cachetextconv")
			t.Setenv("GIT_CONFIG_VALUE_1", "true")
			t.Setenv("GIT_CONFIG_PARAMETERS", "'diff.dstprefix'='parameter/' 'diff."+driver+".cachetextconv'='true'")
			srv, d := testServer(t)
			repo := t.TempDir()
			runGit(t, repo, "init", "-b", "master")
			runGit(t, repo, "config", "user.name", "test")
			runGit(t, repo, "config", "user.email", "test@example.com")
			runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
			testutil.NoError(t, os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.foo diff="+driver+"\n"), 0o600))
			testutil.NoError(t, os.WriteFile(filepath.Join(repo, "data.foo"), []byte("PAYLOAD\n"), 0o600))
			testutil.NoError(t, os.WriteFile(filepath.Join(repo, "render.sh"), []byte("#!/bin/sh\ncat \"$1\"\n"), 0o700))
			runGit(t, repo, "config", "diff."+driver+".textconv", "./render.sh")
			runGit(t, repo, "config", "diff."+driver+".cachetextconv", "true")
			output := func(args ...string) (string, error) {
				cmd := exec.Command("git", args...)
				cmd.Dir = repo
				out, err := cmd.CombinedOutput()
				return string(out), err
			}
			before, err := output("for-each-ref", "--format=%(refname) %(objectname)")
			testutil.NoError(t, err)
			native, err := output("diff", "--no-index", "--", "/dev/null", "data.foo")
			var exit *exec.ExitError
			testutil.True(t, errors.As(err, &exit))
			if exit == nil {
				t.Fatal("native diff did not report differences")
			}
			testutil.Equal(t, exit.ExitCode(), 1)
			testutil.Contains(t, native, "+PAYLOAD\n")
			task := &model.Task{Name: "cached-diff", Status: model.StatusPending, Worktree: repo}
			testutil.NoError(t, d.Add(task))
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/diff?path=data.foo", ""))
			testutil.Equal(t, w.Code, http.StatusOK)
			var got struct{ Diff string }
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			testutil.Equal(t, got.Diff, native)
			after, err := output("for-each-ref", "--format=%(refname) %(objectname)")
			testutil.NoError(t, err)
			testutil.Equal(t, after, before)
			fsck, err := output("fsck", "--no-reflogs")
			t.Logf("git fsck output %q", fsck)
			testutil.NoError(t, err)
		})
	}
}
