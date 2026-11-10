package api

import (
	"database/sql"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentBackstopEpisodes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, d := testServer(t)
	res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	coords, err := d.ListHeraRolesByKind(o.ID, db.HeraKindCoordinator)
	testutil.NoError(t, err)
	w := &model.Task{ID: "worker", Name: "errand", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(w))
	role, _, err := d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "errand", Kind: db.HeraKindWorker}, w.ID, w.Worktree)
	testutil.NoError(t, err)
	state := newIdleWatcherState()
	tail := func(string) []byte { return []byte("Choose one\n❯ 1. yes\n  2. no") }
	for range 3 {
		s.detectNeedsInputTick(state, []string{w.ID}, []string{w.ID}, tail)
	}
	msgs, err := d.HeraInbox(coords[0].ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	_, err = d.MarkHeraMessagesRead(coords[0].ID, []int64{msgs[0].ID})
	testutil.NoError(t, err)
	// Switching from the scraper to a self-reported block is the same episode.
	testutil.NoError(t, d.UpsertHeraRoleStatus(role.ID, db.HeraStatusBlocked))
	s.notifyResidentWorkerPrompts(state, map[string]bool{}, tail)
	msgs, err = d.HeraInbox(coords[0].ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 0)
	testutil.NoError(t, d.ClearBlockedRoleStatus(role.ID))
	s.notifyResidentWorkerPrompts(state, map[string]bool{}, tail)
	testutil.NoError(t, d.UpsertHeraRoleStatus(role.ID, db.HeraStatusBlocked))
	s.notifyResidentWorkerPrompts(state, map[string]bool{}, tail)
	msgs, err = d.HeraInbox(coords[0].ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	testutil.Contains(t, msgs[0].Body, "errand")
	testutil.Contains(t, msgs[0].Body, "Choose one")
	_, err = d.MarkHeraMessagesRead(coords[0].ID, []int64{msgs[0].ID})
	testutil.NoError(t, err)
	testutil.NoError(t, d.ClearBlockedRoleStatus(role.ID))
	s.notifyResidentWorkerPrompts(state, map[string]bool{}, tail)
	s.notifyResidentWorkerPrompts(state, map[string]bool{w.ID: true}, tail)
	msgs, err = d.HeraInbox(coords[0].ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
}

// A transient worker-identity lookup failure mid-episode must keep the
// episode open: once the lookup recovers, the same prompt is not reported
// to the resident a second time ("Backstop notifies once").
func TestResidentBackstopEpisodeSurvivesLookupError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, d := testServer(t)
	res, _, err := d.EnsureResidentTask(db.ResidentWorkDir())
	testutil.NoError(t, err)
	o, err := d.EnsureResidentOrchestrator(res.ID)
	testutil.NoError(t, err)
	coords, err := d.ListHeraRolesByKind(o.ID, db.HeraKindCoordinator)
	testutil.NoError(t, err)
	w := &model.Task{ID: "worker", Name: "errand", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(w))
	_, _, err = d.CreateResidentWorkerRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "errand", Kind: db.HeraKindWorker}, w.ID, w.Worktree)
	testutil.NoError(t, err)
	state := newIdleWatcherState()
	tail := func(string) []byte { return []byte("Choose one\n❯ 1. yes\n  2. no") }
	needs := map[string]bool{w.ID: true}

	s.notifyResidentWorkerPrompts(state, needs, tail)
	msgs, err := d.HeraInbox(coords[0].ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	_, err = d.MarkHeraMessagesRead(coords[0].ID, []int64{msgs[0].ID})
	testutil.NoError(t, err)

	// IsResidentWorker reads task_meta: make only that lookup fail for a tick.
	testutil.NoError(t, d.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE task_meta RENAME TO task_meta_gone`)
		return err
	}))
	s.notifyResidentWorkerPrompts(state, needs, tail)
	testutil.NoError(t, d.WithTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE task_meta_gone RENAME TO task_meta`)
		return err
	}))
	s.notifyResidentWorkerPrompts(state, needs, tail)
	msgs, err = d.HeraInbox(coords[0].ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 0)
}
