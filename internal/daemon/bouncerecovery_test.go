package daemon

import (
	"testing"

	"github.com/argus-foundry/argus/internal/bouncerecovery"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func reattachWithOrphan(t *testing.T, d *Daemon, sup *fakeSupClient) *model.Task {
	t.Helper()
	orphan := &model.Task{Name: "orphan", Project: "proj", Status: model.StatusInProgress}
	testutil.NoError(t, d.db.Add(orphan))
	d.UseSupervisorRunner(sup)
	d.ReconcileOnStartup()
	return orphan
}

func TestReattach_RecordsOrphanInRecoveryLedger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	orphan := reattachWithOrphan(t, d, &fakeSupClient{running: []string{}})

	got, err := bouncerecovery.LoadAll(d.db)
	testutil.NoError(t, err)
	e := got[orphan.ID]
	testutil.Equal(t, e != nil, true)
	testutil.Equal(t, e.Role, bouncerecovery.RolePlain)
	testutil.Equal(t, e.Status, bouncerecovery.StatusPending) // unknown state resumes like mid-turn
	// The legacy signal is unchanged alongside the ledger.
	testutil.Equal(t, hasBounceSignal(t, d, orphan.ID), true)
}

func TestReattach_RecoveryOffWritesNoLedger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	d.cfgFn = func() config.Config {
		c := config.DefaultConfig()
		c.BounceRecovery.Enabled = false
		return c
	}
	orphan := reattachWithOrphan(t, d, &fakeSupClient{running: []string{}})

	got, err := bouncerecovery.LoadAll(d.db)
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 0)
	testutil.Equal(t, hasBounceSignal(t, d, orphan.ID), true)
}

func TestReattach_LiveSetQueryFailureWritesNoLedger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	reattachWithOrphan(t, d, &fakeSupClient{running: nil})
	got, err := bouncerecovery.LoadAll(d.db)
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 0)
}

func TestBounceRecoveryRunner_ResumeAndSettled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, _ := testDaemon(t)
	r := &bounceRecoveryRunner{revive: NewHeraReviveRunner(d.db, d.runner, d.cfgFn), runner: d.runner}
	testutil.Equal(t, r.IsAlive("nope"), false)
	testutil.Equal(t, r.Settled("nope"), false)
	testutil.Equal(t, r.Resume("nope") != nil, true) // unknown task fails loudly
}
