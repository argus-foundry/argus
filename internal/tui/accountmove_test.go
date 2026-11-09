package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/apiclient"
	"github.com/argus-foundry/argus/internal/apistore"
	"github.com/argus-foundry/argus/internal/backendkind"
	"github.com/argus-foundry/argus/internal/claudesession"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/keymap"
	"github.com/argus-foundry/argus/internal/tui/modal"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

var (
	_ remoteAccountMover = (*accountMoveStore)(nil)
	_ remoteAccountMover = (*apistore.Store)(nil)
)

type moveTestSession struct {
	agent.SessionHandle
	idle bool
}

func (*moveTestSession) Alive() bool         { return true }
func (s *moveTestSession) IsIdle() bool      { return s.idle }
func (*moveTestSession) PID() int            { return 77 }
func (*moveTestSession) PTYSize() (int, int) { return 120, 40 }

// moveKickRunner reports one live session and records in-place restarts.
type moveKickRunner struct {
	*agent.Runner
	id     string
	sess   *moveTestSession
	kicked chan *model.Task
}

func (r *moveKickRunner) Get(id string) agent.SessionHandle {
	if id == r.id && r.sess != nil {
		return r.sess
	}
	return nil
}

func (r *moveKickRunner) KickRerender(task *model.Task, _ config.Config, _, _ uint16) error {
	cp := *task
	r.kicked <- &cp
	return nil
}

func (r *moveKickRunner) Recycle(task *model.Task, _ config.Config, _, _ uint16) error {
	cp := *task
	r.kicked <- &cp
	return nil
}

const moveTestTOML = "[accounts.work]\nclaude_config_dir = \"~/work-claude\"\n"

// moveTestDB returns a DB with a Claude backend, a "work" account and a task
// on the default account whose transcript exists under ~/.claude.
func moveTestDB(t *testing.T, toml string) (*db.DB, *model.Task) {
	t.Helper()
	d := testDBWithConfig(t, toml)
	// Keep the backend set deterministic: only the two this test names.
	all, err := d.Backends()
	testutil.NoError(t, err)
	for name := range all {
		testutil.NoError(t, d.DeleteBackend(name))
	}
	testutil.NoError(t, d.SetBackend("cl", config.Backend{Command: "claude"}))
	testutil.NoError(t, d.SetBackend("cx", config.Backend{Command: "codex"}))
	task := &model.Task{ID: "mv1", Name: "mover", Backend: "cl", Worktree: t.TempDir(), SessionID: "sid", Status: model.StatusInProgress, CreatedAt: time.Now()}
	testutil.NoError(t, d.Add(task))
	home, _ := os.UserHomeDir()
	proj, err := claudesession.ProjectDirIn(filepath.Join(home, ".claude"), task.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, os.MkdirAll(proj, 0o700))
	testutil.NoError(t, os.WriteFile(filepath.Join(proj, "sid.jsonl"), []byte(`{"type":"user","sessionId":"sid"}`+"\n"), 0o600))
	return d, task
}

func stubTUIAccountUsage(t *testing.T, snap map[string]usagebudget.Reading) {
	t.Helper()
	t.Cleanup(agent.SetAccountUsageSource(func(config.Config) map[string]usagebudget.Reading { return snap }))
}

func TestUsageDetail(t *testing.T) {
	five, week := 100.0, 74.6
	testutil.Equal(t, usageDetail(&five, &week), "5h 100% · wk 75%")
	testutil.Equal(t, usageDetail(nil, nil), "5h — · wk —")
}

func TestAccountMoveChoices_Local(t *testing.T) {
	now := time.Now()
	stubTUIAccountUsage(t, map[string]usagebudget.Reading{
		"default": {Percentage: 100, FiveHour: &usagebudget.Window{Percentage: 100, ResetAt: now.Add(time.Hour), LastProbedAt: now}},
	})

	t.Run("auto plus the other accounts with usage", func(t *testing.T) {
		d, task := moveTestDB(t, moveTestTOML)
		app := New(d, agent.NewRunner(nil), false)
		choices, notice := app.accountMoveChoices(task)
		testutil.Equal(t, notice, "")
		testutil.DeepEqual(t, choices, []modal.AccountChoice{
			{Name: "auto", Detail: "lowest usage (now work)"},
			{Name: "default", Detail: "5h 100% · wk — ⚠ near limit (current, restarts in place)"},
			{Name: "work", Detail: "5h — · wk —"},
			{Name: "@cx:default", Label: "Codex · default", Detail: handoffDetail, Note: handoffNoteCodex},
		})
	})
	t.Run("Codex task offers Claude handoff rows only", func(t *testing.T) {
		d, task := moveTestDB(t, moveTestTOML)
		task.Backend = "cx"
		app := New(d, agent.NewRunner(nil), false)
		choices, notice := app.accountMoveChoices(task)
		testutil.Equal(t, notice, "")
		testutil.DeepEqual(t, choices, []modal.AccountChoice{
			{Name: "@cl:default", Label: "Claude · default", Detail: handoffDetail + " · 5h 100% · wk — ⚠ near limit", Note: handoffNoteClaud},
			{Name: "@cl:work", Label: "Claude · work", Detail: handoffDetail, Note: handoffNoteClaud},
		})
	})
	t.Run("single Claude account still offers the handoff row", func(t *testing.T) {
		d, task := moveTestDB(t, "")
		app := New(d, agent.NewRunner(nil), false)
		choices, notice := app.accountMoveChoices(task)
		testutil.Equal(t, notice, "")
		testutil.DeepEqual(t, choices, []modal.AccountChoice{
			{Name: "@cx:default", Label: "Codex · default", Detail: handoffDetail, Note: handoffNoteCodex},
		})
	})
	t.Run("unsupported CLI", func(t *testing.T) {
		d, task := moveTestDB(t, moveTestTOML)
		testutil.NoError(t, d.SetBackend("pi", config.Backend{Command: "pi"}))
		task.Backend = "pi"
		app := New(d, agent.NewRunner(nil), false)
		_, notice := app.accountMoveChoices(task)
		testutil.Equal(t, notice, "Only Claude and Codex tasks can move")
	})
}

func TestRemoteAccountMoveChoices(t *testing.T) {
	five := 100.0
	choices, notice := remoteAccountMoveChoices(accountStore{list: []apiclient.AccountJSON{
		{Name: "default", Supports: apiclient.AccountSupportsJSON{Claude: true}, Usage: &apiclient.AccountUsageJSON{FiveHourPct: &five}},
		{Name: "work", Supports: apiclient.AccountSupportsJSON{Claude: true}},
		{Name: "cx", Supports: apiclient.AccountSupportsJSON{Codex: true}},
	}}, "work", config.Config{}, backendkind.Claude)
	testutil.Equal(t, notice, "")
	testutil.DeepEqual(t, choices, []modal.AccountChoice{
		{Name: "auto", Detail: "lowest usage"},
		{Name: "default", Detail: "5h 100% · wk — ⚠ near limit"},
		{Name: "work", Detail: "5h — · wk — (current, restarts in place)"},
	})

	// With backends configured, Codex-capable accounts add handoff rows.
	cfg := config.Config{Backends: map[string]config.Backend{"cx": {Command: "codex"}}}
	choices, _ = remoteAccountMoveChoices(accountStore{list: []apiclient.AccountJSON{
		{Name: "default", Supports: apiclient.AccountSupportsJSON{Claude: true, Codex: true}},
	}}, "default", cfg, backendkind.Claude)
	testutil.DeepEqual(t, choices, []modal.AccountChoice{
		{Name: "@cx:default", Label: "Codex · default", Detail: handoffDetail, Note: handoffNoteCodex},
	})

	_, notice = remoteAccountMoveChoices(accountStore{list: []apiclient.AccountJSON{{Name: "work", Supports: apiclient.AccountSupportsJSON{Claude: true}}}}, "work", config.Config{}, backendkind.Claude)
	testutil.Equal(t, notice, noMoveNotice)
	_, notice = remoteAccountMoveChoices(failingAccountStore{}, "work", config.Config{}, backendkind.Claude)
	testutil.Contains(t, notice, "could not list accounts")
}

type failingAccountStore struct{ stubStore }

func (failingAccountStore) Accounts(context.Context) ([]apiclient.AccountJSON, error) {
	return nil, errors.New("offline")
}

// accountMoveStore is a remote-shaped store that records moves.
type accountMoveStore struct {
	accountStore
	moved chan string
	err   error
}

func (s accountMoveStore) MoveTaskAccount(_ context.Context, id, account string, force bool) (*apiclient.MoveAccountResp, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.moved <- id + "→" + account
	return &apiclient.MoveAccountResp{From: "default", To: account, Resumed: true, Started: true}, nil
}

func TestSmoke_MoveAccountFromTaskList(t *testing.T) {
	for _, tc := range []struct {
		name string
		idle bool
	}{{"idle agent", true}, {"busy agent is interrupted", false}} {
		t.Run(tc.name, func(t *testing.T) {
			stubTUIAccountUsage(t, nil)
			d, task := moveTestDB(t, moveTestTOML)
			r := &moveKickRunner{Runner: agent.NewRunner(nil), id: task.ID, sess: &moveTestSession{idle: tc.idle}, kicked: make(chan *model.Task, 1)}
			app := New(d, r, false)
			sim, stop := wireApp(t, app)
			defer stop()

			sim.InjectKey(tcell.KeyRune, '>', 0)
			waitForCond(t, app.tapp, "account picker open", func() bool { return app.mode == modeAccountPicker && app.accountPicker != nil })
			readUI(t, app.tapp, func() {
				testutil.Equal(t, app.accountPickerForce, !tc.idle)
			})
			sim.InjectKey(tcell.KeyDown, 0, 0) // auto → default (current)
			sim.InjectKey(tcell.KeyDown, 0, 0) // → work
			sim.InjectKey(tcell.KeyEnter, 0, 0)

			select {
			case kicked := <-r.kicked:
				testutil.Equal(t, kicked.Account, "work")
			case <-time.After(5 * time.Second):
				t.Fatal("no in-place restart")
			}
			waitForCond(t, app.tapp, "picker closed", func() bool { return app.mode != modeAccountPicker && app.accountPicker == nil })
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Account, "work")
		})
	}
}

func TestSmoke_MoveAccountPickerCancel(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	d, task := moveTestDB(t, moveTestTOML)
	app := New(d, agent.NewRunner(nil), false)
	sim, stop := wireApp(t, app)
	defer stop()

	sim.InjectKey(tcell.KeyRune, '>', 0)
	waitForCond(t, app.tapp, "account picker open", func() bool { return app.accountPicker != nil })
	sim.InjectKey(tcell.KeyEscape, 0, 0)
	waitForCond(t, app.tapp, "picker closed", func() bool { return app.accountPicker == nil && app.mode == modeTaskList })
	got, _ := d.Get(task.ID)
	testutil.Equal(t, got.Account, "")
}

func TestSmoke_MoveAccountNoticeForUnsupportedCLI(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	d, task := moveTestDB(t, moveTestTOML)
	testutil.NoError(t, d.SetBackend("pi", config.Backend{Command: "pi"}))
	task.Backend = "pi"
	testutil.NoError(t, d.Update(task))
	app := New(d, agent.NewRunner(nil), false)
	sim, stop := wireApp(t, app)
	defer stop()

	sim.InjectKey(tcell.KeyRune, '>', 0)
	waitForCond(t, app.tapp, "notice shown", func() bool { return app.header.Notice() == "Only Claude and Codex tasks can move" })
	readUI(t, app.tapp, func() { testutil.Nil(t, app.accountPicker) })
}

func TestRunAccountMove_ExitedSessionStartsLocally(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	d, task := moveTestDB(t, moveTestTOML)
	// No live session: the move reports Started=false and the TUI starts it.
	// Use a backend that exits immediately so the real start is harmless.
	starts := make(chan *model.Task, 1)
	r := &recordStartRunner{Runner: agent.NewRunner(nil), started: starts}
	app := New(d, r, false)
	_, stop := wireApp(t, app)
	defer stop()

	app.tapp.QueueUpdateDraw(func() { app.runAccountMove(task, "work", false) })
	select {
	case started := <-starts:
		testutil.Equal(t, started.Account, "work")
	case <-time.After(5 * time.Second):
		t.Fatal("session not started")
	}
}

// recordStartRunner records Start without spawning anything.
type recordStartRunner struct {
	*agent.Runner
	started chan *model.Task
}

func (r *recordStartRunner) Start(task *model.Task, _ config.Config, _, _ uint16, _ bool) (agent.SessionHandle, error) {
	cp := *task
	r.started <- &cp
	return &moveTestSession{idle: true}, nil
}

func TestRunAccountMove_Remote(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	store := accountMoveStore{moved: make(chan string, 1)}
	app := New(store, agent.NewRunner(nil), false)
	_, stop := wireApp(t, app)
	defer stop()

	app.tapp.QueueUpdateDraw(func() { app.runAccountMove(&model.Task{ID: "r1", Name: "remote"}, "auto", true) })
	select {
	case got := <-store.moved:
		testutil.Equal(t, got, "r1→auto")
	case <-time.After(5 * time.Second):
		t.Fatal("remote move not called")
	}
	waitForCond(t, app.tapp, "done notice", func() bool { return app.header.Notice() == "Moved remote: default → auto" })

	failing := accountMoveStore{err: errors.New("agent is working")}
	app2 := New(failing, agent.NewRunner(nil), false)
	_, stop2 := wireApp(t, app2)
	defer stop2()
	app2.tapp.QueueUpdateDraw(func() { app2.runAccountMove(&model.Task{ID: "r1", Name: "remote"}, "auto", false) })
	waitForCond(t, app2.tapp, "error shown", func() bool { return app2.statusbar.Error() != "" })
}

func TestHeraRailPaletteListsMoveAccount(t *testing.T) {
	d := testDB(t)
	app := New(d, agent.NewRunner(nil), false)
	called := ""
	app.heraPage.OnMoveAccount = func(id string) { called = id }
	reg := app.heraRailActionRegistry()
	// No selection on an empty rail: the action is not offered.
	_, ok := reg[keymap.ActHeraMoveAccount]
	testutil.False(t, ok)
	testutil.Equal(t, called, "")
}

func TestParseHandoffTarget(t *testing.T) {
	b, a, ok := parseHandoffTarget(handoffTarget("cx", "default"))
	testutil.True(t, ok)
	testutil.Equal(t, b, "cx")
	testutil.Equal(t, a, "default")
	for _, bad := range []string{"work", "auto", "@", "@cx", "@:x", "@cx:"} {
		_, _, ok = parseHandoffTarget(bad)
		testutil.False(t, ok)
	}
}

func TestSmoke_MoveToCodexWithHandoff(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	d, task := moveTestDB(t, moveTestTOML)
	r := &moveKickRunner{Runner: agent.NewRunner(nil), id: task.ID, sess: &moveTestSession{idle: true}, kicked: make(chan *model.Task, 1)}
	app := New(d, r, false)
	sim, stop := wireApp(t, app)
	defer stop()

	sim.InjectKey(tcell.KeyRune, '>', 0)
	waitForCond(t, app.tapp, "account picker open", func() bool { return app.mode == modeAccountPicker && app.accountPicker != nil })
	for range 3 { // auto → default → work → Codex handoff row
		sim.InjectKey(tcell.KeyDown, 0, 0)
	}
	sim.InjectKey(tcell.KeyEnter, 0, 0)

	select {
	case recycled := <-r.kicked:
		testutil.Equal(t, recycled.Backend, "cx")
		testutil.Equal(t, recycled.SessionID, "")
		testutil.Contains(t, recycled.Prompt, "did NOT carry over")
	case <-time.After(5 * time.Second):
		t.Fatal("no handoff restart")
	}
	waitForCond(t, app.tapp, "picker closed", func() bool { return app.mode != modeAccountPicker && app.accountPicker == nil })
	waitForCond(t, app.tapp, "done notice", func() bool {
		return strings.Contains(app.header.Notice(), "conversation not carried over")
	})
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Backend, "cx")
}

func TestRunAccountMove_RemoteHandoff(t *testing.T) {
	stubTUIAccountUsage(t, nil)
	store := backendMoveStore{accountMoveStore: accountMoveStore{moved: make(chan string, 1)}}
	app := New(store, agent.NewRunner(nil), false)
	_, stop := wireApp(t, app)
	defer stop()

	app.tapp.QueueUpdateDraw(func() {
		app.runAccountMove(&model.Task{ID: "r1", Name: "remote"}, handoffTarget("cx", "default"), false)
	})
	select {
	case got := <-store.moved:
		testutil.Equal(t, got, "r1→cx/default")
	case <-time.After(5 * time.Second):
		t.Fatal("remote handoff not called")
	}
	waitForCond(t, app.tapp, "done notice", func() bool {
		return strings.Contains(app.header.Notice(), "with handoff")
	})
}

type backendMoveStore struct{ accountMoveStore }

func (s backendMoveStore) MoveTaskBackend(_ context.Context, id, backend, account string, _ bool) (*apiclient.MoveAccountResp, error) {
	s.moved <- id + "→" + backend + "/" + account
	return &apiclient.MoveAccountResp{ToBackend: backend, Handoff: true, Started: true}, nil
}
