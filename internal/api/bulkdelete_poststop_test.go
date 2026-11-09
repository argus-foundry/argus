package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type postStopDeleteOps struct {
	gitDeleteOps
	session       agent.SessionHandle
	failAfterStop bool
	checks        int
}

func (o *postStopDeleteOps) IsDirty(path string) (bool, error) {
	o.checks++
	if o.failAfterStop && !o.session.Alive() {
		return false, errors.New("post-exit dirty check unavailable")
	}
	return o.gitDeleteOps.IsDirty(path)
}

func TestBulkDelete_PostExitDirtyCheck(t *testing.T) {
	for _, tc := range []struct {
		name, exitAction, skip string
		checkError, allowDirty bool
	}{
		{name: "save during stop", exitAction: "dirty", skip: "dirty_worktree"},
		{name: "check error after stop", exitAction: "clean", checkError: true, skip: "dirty_check_failed"},
		{name: "clean after stop", exitAction: "clean"},
		{name: "allow dirty", exitAction: "dirty", allowDirty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d, _, _, sink := bulkSetup(t)
			repo, origin := t.TempDir(), filepath.Join(t.TempDir(), "origin.git")
			bulkGit(t, repo, "init", "-b", "master")
			bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
			bulkGit(t, repo, "init", "--bare", origin)
			bulkGit(t, repo, "remote", "add", "origin", origin)
			testutil.NoError(t, d.SetProject("poststop", config.Project{Path: repo}))
			wt, branch := agent.WorktreeDir("poststop", "save"), "argus/save"
			bulkGit(t, repo, "worktree", "add", "-b", branch, wt)
			bulkGit(t, repo, "push", "origin", branch)
			task := &model.Task{Name: "save", Project: "poststop", Worktree: wt, Branch: branch, Status: model.StatusInProgress}
			testutil.NoError(t, d.Add(task))
			proof := filepath.Join(t.TempDir(), "flushed")
			readyR, readyW, err := os.Pipe()
			testutil.NoError(t, err)
			defer readyR.Close()
			defer readyW.Close()
			cmd := exec.Command("/bin/sh", "-c", `trap 'if [ "$2" = dirty ]; then printf valuable > late.txt; fi; printf flushed > "$1"; exit 0' HUP TERM; printf x >&3; while :; do read ignored; done`, "sh", proof, tc.exitAction)
			cmd.Dir = wt
			cmd.ExtraFiles = []*os.File{readyW}
			session, err := agent.StartSession(task.ID, cmd, 24, 80)
			testutil.NoError(t, err)
			defer func() { _ = session.Cmd.Process.Kill(); <-session.Done() }()
			var ready [1]byte
			_, err = io.ReadFull(readyR, ready[:])
			testutil.NoError(t, err)
			log, artifact := agent.SessionLogPath(task.ID), filepath.Join(agent.ArtifactsDir(task.ID), "keep.txt")
			for _, path := range []string{log, artifact} {
				testutil.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
				testutil.NoError(t, os.WriteFile(path, []byte("keep"), 0600))
			}
			_, err = d.UpsertArtifact(&model.Artifact{TaskID: task.ID, Filename: "keep.txt"})
			testutil.NoError(t, err)
			s.runner = stoppingSessionRunner{s.runner, session}
			ops := &postStopDeleteOps{gitDeleteOps: gitDeleteOps{ctx: context.Background()}, session: session, failAfterStop: tc.checkError}
			s.deleteOps = ops
			doc := seedDeleteJob(t, d, "poststop", storedDeleteResult{deleteResultView: deleteResultView{TaskID: task.ID, State: deleteTaskPending, Worktree: cleanupPending, LocalBranch: cleanupPending, RemoteBranch: cleanupPending}})
			doc.Preconditions.AllowDirty = tc.allowDirty
			testutil.NoError(t, s.saveDeleteJob("poststop", deleteJobQueued, doc))
			s.runDeleteJob("poststop")
			testutil.False(t, session.Alive())
			flushed, err := os.ReadFile(proof)
			testutil.NoError(t, err)
			testutil.Equal(t, string(flushed), "flushed")
			job, err := d.GetTaskDeleteJob("poststop")
			testutil.NoError(t, err)
			testutil.Equal(t, job.Status, deleteJobComplete)
			testutil.NoError(t, json.Unmarshal([]byte(job.Results), &doc))
			res := doc.Results[0]
			if tc.skip == "" {
				testutil.Equal(t, res.State, deleteTaskSucceeded)
				testutil.True(t, res.RecordDeleted)
				_, err = d.Get(task.ID)
				testutil.ErrorIs(t, err, db.ErrTaskNotFound)
				for _, path := range []string{wt, log, artifact} {
					_, err = os.Stat(path)
					testutil.True(t, os.IsNotExist(err))
				}
				return
			}
			testutil.Equal(t, res.State, deleteTaskSkipped)
			testutil.False(t, res.RecordDeleted)
			testutil.True(t, res.Error != nil)
			if res.Error != nil {
				testutil.Equal(t, *res.Error, tc.skip)
			}
			for _, outcome := range []string{res.Worktree, res.LocalBranch, res.RemoteBranch} {
				testutil.Equal(t, outcome, cleanupPending)
			}
			if tc.exitAction == "dirty" {
				content, err := os.ReadFile(filepath.Join(wt, "late.txt"))
				testutil.NoError(t, err)
				testutil.Equal(t, string(content), "valuable")
				testutil.NoError(t, os.Remove(filepath.Join(wt, "late.txt")))
			}
			ops.failAfterStop = false
			testutil.NoError(t, s.saveDeleteJob("poststop", deleteJobRunning, doc))
			s.runDeleteJob("poststop")
			testutil.Equal(t, ops.checks, 2)
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
			bulkGit(t, repo, "rev-parse", "--verify", "refs/heads/"+branch)
			bulkGit(t, repo, "ls-remote", "--exit-code", "origin", "refs/heads/"+branch)
			for _, event := range sink.events() {
				testutil.False(t, event.Type == model.EventTypeTaskDeleted)
			}
		})
	}
}
