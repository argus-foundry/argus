package client

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
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
