package api

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBulkDelete_CrossProcessOwnership(t *testing.T) {
	if path := os.Getenv("BULK_CHILD_DB"); path != "" {
		home, homeErr := filepath.EvalSymlinks(os.Getenv("HOME"))
		temp, tempErr := filepath.EvalSymlinks(os.TempDir())
		if homeErr != nil || tempErr != nil || !strings.HasPrefix(home, temp+string(os.PathSeparator)) || path != filepath.Join(os.Getenv("HOME"), "shared.sqlite") {
			t.Fatal("child database must belong to an isolated temporary HOME")
		}
		d, err := db.Open(path)
		testutil.NoError(t, err)
		defer func() { testutil.NoError(t, d.Close()) }()
		_, _, err = agent.CreateAndStart(d, ownershipRunner{}, agent.CreateInput{Project: "process", Name: "reuse", Backend: "test", OnWorktreeCreated: func(string) error {
			ready, resume := os.NewFile(3, "ready"), os.NewFile(4, "resume")
			defer ready.Close()
			defer resume.Close()
			if _, err := ready.Write([]byte{1}); err != nil {
				return err
			}
			var b [1]byte
			_, err := io.ReadFull(resume, b[:])
			return err
		}})
		testutil.NoError(t, err)
		return
	}
	t.Setenv("HOME", t.TempDir())
	repo, origin := t.TempDir(), filepath.Join(t.TempDir(), "origin.git")
	bulkGit(t, repo, "init", "-b", "master")
	bulkGit(t, repo, "commit", "--allow-empty", "-m", "init")
	bulkGit(t, repo, "init", "--bare", origin)
	bulkGit(t, repo, "remote", "add", "origin", origin)
	path := filepath.Join(os.Getenv("HOME"), "shared.sqlite")
	d, err := db.Open(path)
	testutil.NoError(t, err)
	defer func() { testutil.NoError(t, d.Close()) }()
	testutil.NoError(t, d.SetProject("process", config.Project{Path: repo, Branch: "HEAD"}))
	testutil.NoError(t, d.SetBackend("test", config.Backend{Command: "echo"}))
	readyR, readyW, err := os.Pipe()
	testutil.NoError(t, err)
	defer readyR.Close()
	defer readyW.Close()
	resumeR, resumeW, err := os.Pipe()
	testutil.NoError(t, err)
	defer resumeR.Close()
	defer resumeW.Close()
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBulkDelete_CrossProcessOwnership$") //nolint:gosec // G702: rerun this test executable with fixed arguments.
	cmd.Env = append(os.Environ(), "BULK_CHILD_DB="+path)
	cmd.ExtraFiles = []*os.File{readyW, resumeR}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	testutil.NoError(t, cmd.Start())
	_ = readyW.Close()
	_ = resumeR.Close()
	waited := false
	defer func() {
		if !waited {
			stop()
			_ = cmd.Wait()
		}
	}()
	var b [1]byte
	_, err = io.ReadFull(readyR, b[:])
	testutil.NoError(t, err)
	wt, branch := agent.WorktreeDir("process", "reuse"), "argus/reuse"
	sentinel := filepath.Join(wt, "keep")
	testutil.NoError(t, os.WriteFile(sentinel, []byte("new owner"), 0600))
	bulkGit(t, repo, "push", "origin", branch)
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s := &Server{db: d, runner: agent.NewRunner(nil), deleteJobCtx: waitCtx}
	res := storedDeleteResult{deleteResultView: deleteResultView{TaskID: "old", RecordDeleted: true}, Snap: &deleteSnap{Worktree: wt, Branch: branch, RepoDir: repo}}
	s.cleanupDeletedTask(&res, gitDeleteOps{ctx: waitCtx})
	testutil.Equal(t, res.State, deleteTaskFailed)
	_, err = os.Stat(sentinel)
	testutil.NoError(t, err)
	bulkGit(t, repo, "rev-parse", "--verify", "refs/heads/"+branch)
	bulkGit(t, repo, "--git-dir", origin, "rev-parse", "--verify", "refs/heads/"+branch)
	_, err = resumeW.Write([]byte{1})
	testutil.NoError(t, err)
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Fatalf("creator: %v\n%s", err, output.String())
	}
	tasks, err := d.TasksChecked()
	testutil.NoError(t, err)
	testutil.Equal(t, len(tasks), 1)
	fresh := &Server{db: d, runner: agent.NewRunner(nil)}
	fresh.cleanupDeletedTask(&res, gitDeleteOps{ctx: context.Background()})
	_, err = os.Stat(sentinel)
	testutil.NoError(t, err)
	bulkGit(t, repo, "rev-parse", "--verify", "refs/heads/"+branch)
	bulkGit(t, repo, "--git-dir", origin, "rev-parse", "--verify", "refs/heads/"+branch)
}
