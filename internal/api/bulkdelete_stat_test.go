package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBulkDelete_StatFailurePreservesResources(t *testing.T) {
	for _, phase := range []string{"preflight", "post-exit"} {
		t.Run(phase, func(t *testing.T) {
			s, d, _, _, sink := bulkSetup(t)
			repo, origin := t.TempDir(), filepath.Join(t.TempDir(), "origin.git")
			bulkGit(t, repo, "init", "-b", "master")
			bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
			bulkGit(t, repo, "init", "--bare", origin)
			bulkGit(t, repo, "remote", "add", "origin", origin)
			testutil.NoError(t, d.SetProject("stat", config.Project{Path: repo}))
			wt, branch := agent.WorktreeDir("stat", "save"), "argus/save"
			bulkGit(t, repo, "worktree", "add", "-b", branch, wt)
			bulkGit(t, repo, "push", "origin", branch)
			task := &model.Task{Name: "save", Project: "stat", Worktree: wt, Branch: branch, Status: model.StatusInProgress}
			testutil.NoError(t, d.Add(task))
			log, artifact := agent.SessionLogPath(task.ID), filepath.Join(agent.ArtifactsDir(task.ID), "keep")
			for _, path := range []string{log, artifact} {
				testutil.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
				testutil.NoError(t, os.WriteFile(path, []byte("keep"), 0600))
			}
			_, err := d.UpsertArtifact(&model.Artifact{TaskID: task.ID, Filename: "keep"})
			testutil.NoError(t, err)
			parent := filepath.Dir(wt)
			t.Cleanup(func() { testutil.NoError(t, os.Chmod(parent, 0700)) }) //nolint:gosec // Restore fixture directory traversal for cleanup.
			denyStat := func() {
				testutil.NoError(t, os.Chmod(parent, 0))
				_, err := os.Stat(wt)
				testutil.True(t, os.IsPermission(err))
			}
			stops := 0
			s.runner = observingDeleteRunner{s.runner, func(string) error {
				stops++
				denyStat()
				return nil
			}}
			if phase == "preflight" {
				denyStat()
			}
			s.deleteOps = gitDeleteOps{ctx: context.Background()}
			doc := seedDeleteJob(t, d, "stat", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending, Worktree: cleanupPending, LocalBranch: cleanupPending, RemoteBranch: cleanupPending}})
			testutil.NoError(t, s.processDeleteTask("stat", &doc, &doc.Results[0]))
			res := doc.Results[0]
			testutil.Equal(t, res.State, deleteTaskSkipped)
			testutil.False(t, res.RecordDeleted)
			testutil.True(t, res.Error != nil)
			if res.Error != nil {
				testutil.Equal(t, *res.Error, "dirty_check_failed")
			}
			for _, outcome := range []string{res.Worktree, res.LocalBranch, res.RemoteBranch} {
				testutil.Equal(t, outcome, cleanupPending)
			}
			wantStops := 0
			if phase == "post-exit" {
				wantStops = 1
			}
			testutil.Equal(t, stops, wantStops)
			_, err = d.Get(task.ID)
			testutil.NoError(t, err)
			for _, path := range []string{log, artifact} {
				content, err := os.ReadFile(path)
				testutil.NoError(t, err)
				testutil.Equal(t, string(content), "keep")
			}
			artifacts, err := d.Artifacts(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(artifacts), 1)
			testutil.NoError(t, os.Chmod(parent, 0700)) //nolint:gosec // Restore fixture directory traversal for assertions.
			_, err = os.Stat(wt)
			testutil.NoError(t, err)
			bulkGit(t, repo, "rev-parse", "--verify", "refs/heads/"+branch)
			bulkGit(t, repo, "ls-remote", "--exit-code", "origin", "refs/heads/"+branch)
			for _, event := range sink.events() {
				testutil.False(t, event.Type == model.EventTypeTaskDeleted)
			}
		})
	}
}

func TestBulkDelete_MissingWorktreePassesDirtyChecks(t *testing.T) {
	s, d, _, _, _ := bulkSetup(t)
	task := addTask(t, d, "missing")
	testutil.NoError(t, os.Remove(task.Worktree))
	doc := seedDeleteJob(t, d, "missing", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending}})
	testutil.NoError(t, s.processDeleteTask("missing", &doc, &doc.Results[0]))
	testutil.Equal(t, doc.Results[0].State, deleteTaskSucceeded)
	testutil.True(t, doc.Results[0].RecordDeleted)
}
