package daemon

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestSupervisorWorkerIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	w := &model.Task{ID: "worker", Name: "worker", Backend: "claude", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(w))
	_, binding, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Kind: db.HeraKindWorker, Name: "worker"}, w.ID, w.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetArchived(w.ID, true))
	testutil.NoError(t, d.SetArchived(w.ID, false))
	s := NewSupervisor(d.Config, d.IsResidentWorker)
	var started StartResp
	testutil.NoError(t, s.StartSession(&StartReq{TaskID: w.ID, Backend: w.Backend, Worktree: w.Worktree}, &started))
	testutil.Contains(t, started.Error, "requires the Argus sandbox")
	for _, kick := range []struct {
		name string
		fn   func(*KickReq, *StatusResp) error
	}{{"kick", s.KickRerender}, {"width", s.KickWidthRerender}} {
		t.Run(kick.name, func(t *testing.T) {
			var resp StatusResp
			testutil.NoError(t, kick.fn(&KickReq{TaskID: w.ID, Backend: w.Backend, Worktree: w.Worktree}, &resp))
			testutil.Contains(t, resp.Error, "requires the Argus sandbox")
		})
	}
	var recycled StatusResp
	testutil.NoError(t, s.Recycle(&RecycleReq{TaskID: w.ID, Backend: w.Backend, Worktree: w.Worktree}, &recycled))
	testutil.Contains(t, recycled.Error, "requires the Argus sandbox")
	_, err = d.GraduateHeraWorker(binding.ID)
	testutil.NoError(t, err)
	// No live session: the ordinary kick path now reports session-not-found,
	// proving it no longer fails the unattended sandbox preflight.
	var normal StatusResp
	testutil.NoError(t, s.KickRerender(&KickReq{TaskID: w.ID}, &normal))
	testutil.Contains(t, normal.Error, "session not found")
}
