package client

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSupStopPID(t *testing.T) {
	d, sc, database := supE2E(t)
	sock := filepath.Join(t.TempDir(), "d.sock")
	serveTestServer(t, d, sock)
	waitFile(t, sock)
	c, err := Connect(sock)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	task := startE2E(t, d, sc, database, "reclaim-stop", "printf 'ready\\n'; exec sleep 30")
	sess := sc.Get(task.ID)
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(string(sess.RecentOutput()), "ready") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	testutil.Contains(t, string(sess.RecentOutput()), "ready")
	pid := sess.PID()
	testutil.ErrorIs(t, c.CheckReclaimSessionRetired(task.ID), agent.ErrSessionStarting)
	testutil.ErrorIs(t, c.StopTreeForPID(task.ID, pid+1), agent.ErrSessionNotFound)
	info, err := sc.ProbeSession(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, info.Alive)
	testutil.Equal(t, info.PID, pid)

	testutil.NoError(t, c.StopTreeForPID(task.ID, pid))
	waitStatus(t, database, task.ID, model.StatusInReview)
	deadline = time.Now().Add(3 * time.Second)
	for {
		err = c.CheckReclaimSessionRetired(task.ID)
		if err == nil || time.Now().After(deadline) {
			break
		}
		testutil.ErrorIs(t, err, agent.ErrSessionStarting)
		time.Sleep(10 * time.Millisecond)
	}
	testutil.NoError(t, err)
}

func TestSupReclaimBoot(t *testing.T) {
	d, sc, database := supE2E(t)
	task := startE2E(t, d, sc, database, "reclaim-boot", "printf 'ready\\n'; exec sleep 30")
	sess := sc.Get(task.ID)
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(string(sess.RecentOutput()), "ready") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	testutil.Contains(t, string(sess.RecentOutput()), "ready")
	pid := sess.PID()
	testutil.NoError(t, database.RecordResumedSession(task.ID, pid, "", ""))
	testutil.NoError(t, database.SetArchived(task.ID, true))
	armed, err := database.ArmHeraReclaimStop(task.ID, pid)
	testutil.NoError(t, err)
	testutil.True(t, armed)

	// The old daemon misses the normal stream relay. Its supervisor still
	// owns the exact receipt when a new daemon connects.
	exits := make(chan daemon.ExitInfo, 1)
	sc.OnSessionExitOwned(func(_ string, info daemon.ExitInfo, _ agent.SessionHandle) { exits <- info })
	testutil.NoError(t, sc.StopTreeForPID(task.ID, pid))
	select {
	case info := <-exits:
		testutil.Equal(t, info.PID, pid)
		testutil.True(t, info.Stopped)
		testutil.False(t, info.StreamLost)
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not observe the stopped process")
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		err = sc.CheckReclaimSessionRetired(task.ID)
		if err == nil || time.Now().After(deadline) {
			break
		}
		testutil.ErrorIs(t, err, agent.ErrSessionStarting)
		time.Sleep(10 * time.Millisecond)
	}
	testutil.NoError(t, err)
	before, err := database.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, before.Status, model.StatusInProgress)

	missing := &model.Task{ID: "no-receipt", Name: "No receipt", Archived: true, Status: model.StatusInProgress}
	testutil.NoError(t, database.Add(missing))
	armed, err = database.ArmHeraReclaimStop(missing.ID, pid)
	testutil.NoError(t, err)
	testutil.True(t, armed)
	newClient, err := Connect(sc.sockPath)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = newClient.Close() })
	newDaemon := daemon.New(database)
	newDaemon.UseSupervisorRunner(newClient)
	newDaemon.ReconcileOnStartup()

	after, err := database.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, after.Status, model.StatusComplete)
	armed, err = database.HeraReclaimStopPending(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, armed)
	var exit daemon.ExitInfo
	testutil.NoError(t, newDaemon.GetExitInfo(&daemon.TaskIDReq{TaskID: task.ID, ExpectedPID: pid}, &exit))
	testutil.Equal(t, exit.PID, pid)
	testutil.True(t, exit.Stopped)
	testutil.False(t, exit.StreamLost)

	after, err = database.Get(missing.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, after.Status, model.StatusInProgress)
	armed, err = database.HeraReclaimStopPending(missing.ID)
	testutil.NoError(t, err)
	testutil.True(t, armed)

	orch, err := database.CreateHeraOrchestrator("reclaim-boot", "master")
	testutil.NoError(t, err)
	_, binding, err := database.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: orch.ID,
		Name:           "worker",
		Kind:           db.HeraKindWorker,
	}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, database.EndHeraBinding(binding.ID, db.HeraEndReasonUserDeleted))
	newDaemon.ReconcileOnStartup()
	_, err = database.Get(task.ID)
	testutil.ErrorIs(t, err, db.ErrTaskNotFound)
}
