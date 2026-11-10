package api

import (
	"database/sql"
	"fmt"
	"strings"
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

// The notice tail is plain screen text: escape sequences are stripped before
// the 600-rune cap, so their printable remnants never push the real question
// and options out of the message.
func TestResidentBackstopTailIsPlainText(t *testing.T) {
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

	// Many redraw frames (each full of CSI/OSC noise) precede the prompt, so a
	// raw-byte tail would be all escape remnants.
	var raw strings.Builder
	for range 100 {
		raw.WriteString("\x1b[?2026h\x1b[2K\x1b[1A\x1b[2K\x1b[1A\x1b[38;2;215;119;87m\xe2\x9c\xb3\x1b[39m\x1b]0;\xe2\x9c\xb3 Claude Code\x07\x1b[?2026l\r\n")
	}
	raw.WriteString("\x1b[1m Do you want to proceed?\x1b[22m\r\n\x1b[36m\xe2\x9d\xaf 1. Yes\x1b[39m\r\n  2. No\r\n\x1b[2K\x1b[G")
	tail := func(string) []byte { return []byte(raw.String()) }

	s.notifyResidentWorkerPrompts(newIdleWatcherState(), map[string]bool{w.ID: true}, tail)
	msgs, err := d.HeraInbox(coords[0].ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	for _, want := range []string{"Do you want to proceed?", "1. Yes", "2. No"} {
		testutil.Contains(t, msgs[0].Body, want)
	}
	for _, bad := range []string{"[2K", "[1A", "[38;", "]0;", "[?2026", "\x1b"} {
		testutil.False(t, strings.Contains(msgs[0].Body, bad))
	}
}

// A failed task read (not the identity lookup) for one tick also keeps the
// episode open, so the recovered tick does not re-notify.
func TestResidentBackstopEpisodeSurvivesTaskReadError(t *testing.T) {
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

	rename := func(from, to string) {
		testutil.NoError(t, d.WithTx(func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE tasks RENAME COLUMN ` + from + ` TO ` + to)
			return err
		}))
	}
	rename("prompt", "prompt_gone")
	// Identity (task_meta + hera tables) still works while the task read fails.
	is, err := d.IsResidentWorker(w.ID)
	testutil.NoError(t, err)
	testutil.True(t, is)
	s.notifyResidentWorkerPrompts(state, needs, tail)
	rename("prompt_gone", "prompt")
	s.notifyResidentWorkerPrompts(state, needs, tail)
	msgs, err = d.HeraInbox(coords[0].ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 0)
}

func TestResidentPromptTail(t *testing.T) {
	t.Run("colour codes do not eat the rune budget", func(t *testing.T) {
		// Plain text is well under 600 chars; the raw bytes are far over.
		var b strings.Builder
		b.WriteString("\x1b[1;38;5;208mDo you want to proceed?\x1b[0m\n")
		for range 100 {
			b.WriteString("\x1b[2K\x1b[1G")
		}
		b.WriteString("\x1b[38;5;10m❯ 1. Yes\x1b[0m\n\x1b[38;5;9m  2. last option\x1b[0m")
		raw := b.String()
		testutil.True(t, len(raw) > 600)
		got := residentPromptTail([]byte(raw))
		testutil.Equal(t, got, "Do you want to proceed?\n❯ 1. Yes\n  2. last option")
	})
	t.Run("caps at 600 runes keeping the end", func(t *testing.T) {
		got := residentPromptTail([]byte(strings.Repeat("x", 2000) + "END"))
		testutil.Equal(t, len([]rune(got)), 600)
		testutil.True(t, strings.HasSuffix(got, "END"))
	})
	t.Run("keeps the last 14 non-blank lines", func(t *testing.T) {
		var lines []string
		for i := range 20 {
			lines = append(lines, fmt.Sprintf("line%02d", i))
		}
		got := residentPromptTail([]byte(strings.Join(lines, "\r\n")))
		testutil.Equal(t, got, strings.Join(lines[6:], "\n"))
	})
	t.Run("bare carriage return splits lines", func(t *testing.T) {
		testutil.Equal(t, residentPromptTail([]byte("foo\rbar")), "foo\nbar")
	})
}
