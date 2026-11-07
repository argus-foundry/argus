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

func TestHandleGitDiffNonRepo(t *testing.T) {
	srv, d := testServer(t)
	mux := srv.routes()
	for _, missing := range []bool{false, true} {
		name := "existing directory"
		if missing {
			name = "missing directory"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("not a repo\n"), 0o600))
			if missing {
				dir = filepath.Join(dir, "missing")
			}
			task := &model.Task{Name: "invalid", Status: model.StatusPending, Worktree: dir}
			testutil.NoError(t, d.Add(task))
			for _, path := range []string{"plain.txt", "missing.txt"} {
				t.Run(path, func(t *testing.T) {
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/diff?path="+path, ""))
					testutil.Equal(t, w.Code, http.StatusInternalServerError)
				})
			}
		})
	}
}

func TestHandleGitDiffComparisonMetadata(t *testing.T) {
	srv, d := testServer(t)
	mux := srv.routes()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "master")
	runGit(t, dir, "config", "user.email", "t@t")
	runGit(t, dir, "config", "user.name", "t")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	commit := func(name string) string {
		testutil.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600))
		runGit(t, dir, "add", name)
		runGit(t, dir, "commit", "-q", "-m", name)
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = dir
		out, err := cmd.Output()
		testutil.NoError(t, err)
		return strings.TrimSpace(string(out))
	}
	masterSHA := commit("base.txt")
	runGit(t, dir, "checkout", "-q", "-b", "parent")
	parentSHA := commit("parent.txt")
	runGit(t, dir, "checkout", "-q", "-b", "child")
	commit("child.txt")
	for _, tc := range []struct {
		base     string
		resolved string
		sha      string
		fallback bool
	}{
		{"parent", "parent", parentSHA, false},
		{"missing-parent", "master", masterSHA, true},
	} {
		t.Run(tc.base, func(t *testing.T) {
			task := &model.Task{Name: "stacked", Status: model.StatusPending, Worktree: dir, BaseBranch: tc.base}
			testutil.NoError(t, d.Add(task))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, authedReq("GET", "/api/tasks/"+task.ID+"/git/diff?path=child.txt", ""))
			testutil.Equal(t, w.Code, http.StatusOK)
			var got struct {
				Diff       string
				Comparison struct {
					RequestedBase  string          `json:"requested_base"`
					ResolvedBase   string          `json:"resolved_base"`
					MergeBase      string          `json:"merge_base"`
					FallbackReason json.RawMessage `json:"fallback_reason"`
				}
			}
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			testutil.Contains(t, got.Diff, "+child.txt\n")
			testutil.Equal(t, got.Comparison.RequestedBase, tc.base)
			testutil.Equal(t, got.Comparison.ResolvedBase, tc.resolved)
			testutil.Equal(t, got.Comparison.MergeBase, tc.sha)
			if tc.fallback {
				var reason string
				testutil.NoError(t, json.Unmarshal(got.Comparison.FallbackReason, &reason))
				testutil.NotEqual(t, reason, "")
			} else {
				testutil.Equal(t, string(got.Comparison.FallbackReason), "null")
			}
		})
	}
}
