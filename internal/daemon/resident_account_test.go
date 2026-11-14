package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/claudesession"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

// moveHandle is a live resident session with a settable idle flag.
type moveHandle struct {
	liveHandle
	idle bool
	tail []byte // RecentOutputTail; nil keeps the handle raw-idle-only
}

func (h *moveHandle) RecentOutputTail(int) []byte { return h.tail }

func (h *moveHandle) IsIdle() bool        { return h.idle }
func (h *moveHandle) PTYSize() (int, int) { return 100, 40 }

// moverRunner wraps fakeResRunner with the kick/recycle surface a move needs.
type moverRunner struct {
	*fakeResRunner
	kicked, recycled []model.Task
}

func (r *moverRunner) KickRerender(t *model.Task, _ config.Config, _, _ uint16) error {
	r.kicked = append(r.kicked, *t)
	return nil
}
func (r *moverRunner) Recycle(t *model.Task, _ config.Config, _, _ uint16) error {
	r.recycled = append(r.recycled, *t)
	return nil
}

type switchEnv struct {
	*resEnv
	t     *testing.T
	mover *moverRunner
	usage map[string]usagebudget.Reading
	notif *fakeResNotifier
	task  *model.Task
	h     *moveHandle
}

func win(pct float64, now time.Time) *usagebudget.Window {
	return &usagebudget.Window{Percentage: pct, ResetAt: now.Add(2 * time.Hour), LastProbedAt: now}
}

// newSwitchEnv builds a started resident on the default account, with a
// second Claude account "work", usage stubbed to 100% on default.
func newSwitchEnv(t *testing.T) *switchEnv {
	t.Helper()
	e := newResEnv(t)
	home := t.TempDir()
	e.cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: filepath.Join(home, "work")}}
	e.cfg.Resident.Accounts = []string{"default", "work"}
	se := &switchEnv{resEnv: e, t: t, notif: &fakeResNotifier{}}
	se.mover = &moverRunner{fakeResRunner: e.run}
	e.m.mover = se.mover
	e.m.notifier = func() residentNotifier { return se.notif }
	// The real clock is what usage windows are judged against.
	e.m.now = time.Now
	now := time.Now()
	se.usage = map[string]usagebudget.Reading{"work": {Percentage: 20, FiveHour: win(20, now)}}
	t.Cleanup(agent.SetAccountUsageSource(func(config.Config) map[string]usagebudget.Reading { return se.usage }))
	e.m.tick() // first start, on default (the list's first entry)
	// Then default runs out of budget.
	se.usage["default"] = usagebudget.Reading{Percentage: 100, FiveHour: win(100, now)}
	task, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	se.task = task
	// The conversation exists on disk so a move can carry it over.
	proj, perr := claudesession.ProjectDirIn(filepath.Join(os.Getenv("HOME"), ".claude"), task.Worktree)
	testutil.NoError(t, perr)
	testutil.NoError(t, os.MkdirAll(proj, 0o700))
	testutil.NoError(t, os.WriteFile(filepath.Join(proj, task.SessionID+".jsonl"), []byte(`{"type":"user","sessionId":"`+task.SessionID+`"}`+"\n"), 0o600))
	h := &moveHandle{liveHandle: liveHandle{alive: true}, idle: true}
	e.run.sess[task.ID] = &h.liveHandle
	se.setSession(h)
	return se
}

// setSession replaces the fake runner's session with h (Get returns the
// *liveHandle map entry, so wrap through the mover's Get instead).
func (se *switchEnv) setSession(h *moveHandle) {
	se.m.runner = &sessRunner{moverRunner: se.mover, h: h}
	se.m.mover = &sessRunner{moverRunner: se.mover, h: h}
	se.h = h
}

type sessRunner struct {
	*moverRunner
	h *moveHandle
}

func (r *sessRunner) Get(string) agent.SessionHandle { return r.h }

func (se *switchEnv) boardItems(t *testing.T) []*db.HeraBoardItem {
	t.Helper()
	orch, err := se.db.EnsureResidentOrchestrator(se.task.ID)
	testutil.NoError(t, err)
	roles, err := se.db.ListHeraRolesByKind(orch.ID, db.HeraKindCoordinator)
	testutil.NoError(t, err)
	items, err := se.db.ListHeraBoardItems(roles[0].ID, true)
	testutil.NoError(t, err)
	return items
}

func (se *switchEnv) tickN(n int) {
	for i := 0; i < n; i++ {
		se.m.tick()
	}
}

// recovered gives name a fresh reading with plenty of headroom.
func (se *switchEnv) recovered(name string) {
	now := time.Now()
	se.usage[name] = usagebudget.Reading{Percentage: 5, FiveHour: win(5, now)}
}

func (se *switchEnv) moved() bool { return len(se.mover.kicked)+len(se.mover.recycled) > 0 }

func TestAutoSwitch_DepletedIdleMovesAfterConfirmTicks(t *testing.T) {
	se := newSwitchEnv(t)
	se.tickN(1)
	testutil.False(t, se.moved()) // one tick is not enough
	se.tickN(1)
	testutil.True(t, se.moved())

	row, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, row.Account, "work")
	ov, ok, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, ov.By, db.OverrideByAuto)

	// Solo is told to carry on; the human gets a board item.
	testutil.Equal(t, len(se.notif.calls), 1)
	testutil.Contains(t, se.notif.calls[0].text, "Continue whatever you were doing")
	testutil.Contains(t, se.notif.calls[0].text, "default to work")
	items := se.boardItems(t)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Kind, "fyi")
	testutil.Contains(t, items[0].Subject, "default to work")
}

func TestAutoSwitch_DoesNotInterruptATurn(t *testing.T) {
	se := newSwitchEnv(t)
	se.h.idle = false
	se.tickN(5)
	testutil.False(t, se.moved())
	se.h.idle = true
	se.tickN(2)
	testutil.True(t, se.moved())
}

func TestAutoSwitch_NeedsInputDoesNotBlock(t *testing.T) {
	se := newSwitchEnv(t)
	se.m.waiting = func(string) bool { return true } // the usage-limit menu
	se.tickN(2)
	testutil.True(t, se.moved())
}

func TestAutoSwitch_UnknownOrStaleReadingNeverTriggers(t *testing.T) {
	se := newSwitchEnv(t)
	delete(se.usage, "default")
	se.tickN(4)
	testutil.False(t, se.moved())
	old := time.Now().Add(-48 * time.Hour)
	se.usage["default"] = usagebudget.Reading{FiveHour: &usagebudget.Window{Percentage: 100, ResetAt: time.Now().Add(time.Hour), LastProbedAt: old}}
	se.tickN(4)
	testutil.False(t, se.moved())
}

func TestAutoSwitch_SettingOff(t *testing.T) {
	se := newSwitchEnv(t)
	se.cfg.Resident.AutoSwitchAccount = false
	se.tickN(4)
	testutil.False(t, se.moved())
}

func TestAutoSwitch_NoEligibleAccountPostsOnceAndRecovers(t *testing.T) {
	se := newSwitchEnv(t)
	se.usage["work"] = usagebudget.Reading{FiveHour: win(100, time.Now())}
	se.tickN(6)
	testutil.False(t, se.moved())
	items := se.boardItems(t)
	testutil.Equal(t, len(items), 1) // one per episode, not per tick
	testutil.Equal(t, items[0].Kind, "decision")
	testutil.Equal(t, len(se.notif.calls), 0)

	se.usage["work"] = usagebudget.Reading{FiveHour: win(10, time.Now())}
	se.tickN(1)
	testutil.True(t, se.moved())
}

func TestAutoSwitch_CooldownSurvivesManagerRestart(t *testing.T) {
	se := newSwitchEnv(t)
	// A switch 3 minutes ago, persisted in the DB.
	ago := time.Now().Add(-3 * time.Minute).UTC().Format(db.ResidentTimeFormat)
	testutil.NoError(t, se.db.SetMeta(se.task.ID, db.ResidentMetaNamespace, db.MetaKeyLastAutoSwitch, ago))
	se.tickN(4)
	testutil.False(t, se.moved())

	old := time.Now().Add(-11 * time.Minute).UTC().Format(db.ResidentTimeFormat)
	testutil.NoError(t, se.db.SetMeta(se.task.ID, db.ResidentMetaNamespace, db.MetaKeyLastAutoSwitch, old))
	se.tickN(1)
	testutil.True(t, se.moved())
}

// restart simulates a crash restart of the resident session.
func (se *switchEnv) restart() startCall {
	delete(se.run.sess, se.task.ID)
	se.m.runner = se.run
	se.m.nextAttempt = time.Time{}
	se.m.tick()
	return se.run.starts[len(se.run.starts)-1]
}

// A restart that resumes a conversation moves off a depleted or delisted account
// through the move core: same conversation, one marker, one notice, one board item.
func TestStart_RestartMovesOffDelistedDepletedAccountKeepingTheConversation(t *testing.T) {
	se := newSwitchEnv(t)
	sid := se.task.SessionID
	testutil.True(t, sid != "")
	se.cfg.Resident.Accounts = []string{"work"} // default (the running account) is delisted and depleted
	got := se.restart()
	testutil.Equal(t, got.task.Account, "work")
	testutil.Equal(t, got.task.SessionID, sid)
	testutil.Equal(t, got.resume, true)
	row, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, row.Account, "work")
	ov, ok, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, ov.By, db.OverrideByAuto)
	testutil.Equal(t, len(se.notif.calls), 1)
	testutil.Contains(t, se.notif.calls[0].text, "Continue whatever you were doing")
	testutil.Equal(t, len(se.boardItems(t)), 1)
	testutil.False(t, se.db.ResidentLastAutoSwitch(se.task.ID).IsZero())
	// The next ordinary restart stays on work: no second move.
	again := se.restart()
	testutil.Equal(t, again.task.Account, "work")
	testutil.Equal(t, len(se.notif.calls), 1)
}

// With a healthy account and no better one, a restart changes nothing.
func TestStart_RestartStaysPutWhenNothingIsBetter(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	got := se.restart()
	testutil.Equal(t, got.task.Account, "")
	testutil.Equal(t, len(se.notif.calls), 0)
	_, ok, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.False(t, ok)
}

// onWork puts the resident on "work" (an earlier auto move), default preferred.
func (se *switchEnv) onWork(by string, ago time.Duration) {
	testutil.NoError(se.t, se.db.SetResidentAccountMove(se.task.ID, "work", se.task.SessionID,
		db.ResidentAccountOverride{Account: "work", By: by}))
	row, _ := se.db.Get(se.task.ID)
	se.task = row
	if ago > 0 {
		old := time.Now().Add(-ago).UTC().Format(db.ResidentTimeFormat)
		testutil.NoError(se.t, se.db.SetMeta(se.task.ID, db.ResidentMetaNamespace, db.MetaKeyLastAutoSwitch, old))
	}
}

func TestStart_RestartPicksPreferredAccountWithAFreshReading(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default") // preferred, fresh reading, room
	se.onWork(db.OverrideByAuto, time.Hour)
	got := se.restart()
	testutil.Equal(t, got.task.Account, "")
	testutil.Equal(t, got.resume, true)
	testutil.Equal(t, got.task.SessionID, se.task.SessionID)
	row, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, row.Account, "")
	testutil.Equal(t, len(se.notif.calls), 1)
	testutil.Contains(t, se.notif.calls[0].text, "back to default")
	items := se.boardItems(t)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Kind, "fyi")
	ov, _, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.Equal(t, ov.By, db.OverrideByAuto)
}

func TestStart_RestartStaysWhenPreferredReadingIsStaleOrUnknown(t *testing.T) {
	se := newSwitchEnv(t)
	se.onWork(db.OverrideByAuto, time.Hour)
	stale := time.Now().Add(-3 * time.Hour)
	se.usage["default"] = usagebudget.Reading{Percentage: 5, FiveHour: &usagebudget.Window{Percentage: 5, ResetAt: time.Now().Add(time.Hour), LastProbedAt: stale}}
	testutil.Equal(t, se.restart().task.Account, "work")
	delete(se.usage, "default")
	testutil.Equal(t, se.restart().task.Account, "work")
	testutil.Equal(t, len(se.notif.calls), 0)
}

// A manual `>` pick is a one-time move: its own restart stays put, and a later
// restart re-picks the best account per the list.
func TestStart_ManualPickDoesNotSurviveARestart(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	clock := time.Now()
	se.m.now = func() time.Time { return clock }
	_, err := se.m.MoveAccount("work", true)
	testutil.NoError(t, err)
	row, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, row.Account, "work")
	// The move's own restart (or a crash right after it) does not re-pick.
	testutil.Equal(t, se.restart().task.Account, "work")
	// Later, the restart re-picks the preferred account (fresh reading, cooldown clear).
	clock = clock.Add(residentOwnRestartWindow + time.Minute)
	se.recovered("default")
	got := se.restart()
	testutil.Equal(t, got.task.Account, "")
	testutil.Equal(t, got.resume, true)
}

// The restart a move performs does not re-run the pick: the cooldown it stamped
// keeps even a recovered preferred account from pulling Solo straight back.
func TestStart_MovesOwnRestartDoesNotMoveAgain(t *testing.T) {
	se := newSwitchEnv(t)
	se.tickN(2) // depleted default -> work, stamps the cooldown
	testutil.True(t, se.moved())
	testutil.Equal(t, len(se.notif.calls), 1)
	se.recovered("default") // preferred recovers right away
	got := se.restart()     // the move's restart (or a crash right after it)
	testutil.Equal(t, got.task.Account, "work")
	testutil.Equal(t, len(se.notif.calls), 1)
	testutil.Equal(t, len(se.boardItems(t)), 1)
}

// A running session with plenty of room never moves, even after the preferred
// account recovers: moving back up happens only at a restart.
func TestAutoSwitch_RunningSessionNeverMovesUp(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	se.onWork(db.OverrideByAuto, time.Hour)
	se.tickN(8)
	testutil.False(t, se.moved())
	row, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, row.Account, "work")
}

func TestRecycle_RepicksAccountWithoutCarryingAConversation(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	se.onWork(db.OverrideByAuto, time.Hour)
	task, _ := se.db.Get(se.task.ID)
	note := se.m.repickForRecycle(task)
	testutil.Contains(t, note, "back to default")
	testutil.Contains(t, note, "no earlier conversation")
	testutil.Equal(t, task.Account, "")
	row, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, row.Account, "")
	testutil.Equal(t, len(se.boardItems(t)), 1)
	testutil.False(t, se.moved()) // no kick/recycle from the move core: the recycle restarts it
	testutil.Equal(t, len(se.notif.calls), 0)
}

func TestRecycle_ManualPickDoesNotSurvive(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	se.onWork(db.OverrideByManual, time.Hour)
	task, _ := se.db.Get(se.task.ID)
	testutil.Contains(t, se.m.repickForRecycle(task), "back to default")
	testutil.Equal(t, task.Account, "")
}

func TestRecycle_RespectsCooldownAndSetting(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	se.onWork(db.OverrideByAuto, time.Minute) // cooldown running
	task, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, se.m.repickForRecycle(task), "")
	old := time.Now().Add(-time.Hour).UTC().Format(db.ResidentTimeFormat)
	testutil.NoError(t, se.db.SetMeta(se.task.ID, db.ResidentMetaNamespace, db.MetaKeyLastAutoSwitch, old))
	se.cfg.Resident.AutoSwitchAccount = false
	testutil.Equal(t, se.m.repickForRecycle(task), "")
}

func TestStart_ReattachKeepsTheRunningAccount(t *testing.T) {
	se := newSwitchEnv(t)
	se.cfg.Resident.Accounts = []string{"work"}
	// The runner still holds a handle for the task, so the start reattaches
	// instead of spawning: the process keeps its account, no re-pick.
	dead := &moveHandle{liveHandle: liveHandle{alive: false}}
	se.run.reattach = true
	se.m.runner = &sessRunner{moverRunner: se.mover, h: dead}
	se.m.nextAttempt = time.Time{}
	se.m.tick()
	row, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, row.Account, "") // DB account still matches the process that is running
	testutil.Equal(t, len(se.notif.calls), 0)
}

func TestStart_FreshStartSkipsDepletedAndMissingListedAccounts(t *testing.T) {
	e := newResEnv(t)
	e.cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: t.TempDir()}}
	e.cfg.Resident.Accounts = []string{"ghost", "default", "work"}
	e.m.now = time.Now // usage windows are judged against the real clock
	now := time.Now()
	usage := map[string]usagebudget.Reading{"default": {Percentage: 100, FiveHour: win(100, now)}}
	t.Cleanup(agent.SetAccountUsageSource(func(config.Config) map[string]usagebudget.Reading { return usage }))
	e.m.tick()
	testutil.Equal(t, e.run.starts[0].task.Account, "work")
	testutil.Equal(t, e.run.starts[0].resume, false)
}

func TestStart_FirstStartUsesListNotDefault(t *testing.T) {
	e := newResEnv(t)
	e.cfg.Accounts = map[string]config.Account{"personal": {ClaudeConfigDir: t.TempDir()}}
	e.cfg.Resident.Accounts = []string{"personal"}
	t.Cleanup(agent.SetAccountUsageSource(func(config.Config) map[string]usagebudget.Reading { return nil }))
	e.m.tick()
	testutil.Equal(t, e.run.starts[0].task.Account, "personal")
}

func TestAutoSwitch_ManualChoiceNotMoved(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	se.onWork(db.OverrideByManual, 0)
	se.tickN(6)
	testutil.False(t, se.moved())
}

func TestAutoSwitch_FollowsListOrderNotLowestUsage(t *testing.T) {
	se := newSwitchEnv(t)
	se.cfg.Accounts["third"] = config.Account{ClaudeConfigDir: filepath.Join(t.TempDir(), "third")}
	se.cfg.Resident.Accounts = []string{"default", "work", "third"}
	now := time.Now()
	se.usage["work"] = usagebudget.Reading{Percentage: 90, FiveHour: win(90, now)}
	se.usage["third"] = usagebudget.Reading{Percentage: 1, FiveHour: win(1, now)}
	se.tickN(2)
	testutil.True(t, se.moved())
	ov, _, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.Equal(t, ov.Account, "work") // next in the list, not third (lowest usage)
}

func TestAutoSwitch_AutoEntryPicksMostHeadroom(t *testing.T) {
	se := newSwitchEnv(t)
	se.cfg.Accounts["third"] = config.Account{ClaudeConfigDir: filepath.Join(t.TempDir(), "third")}
	se.cfg.Resident.Accounts = []string{"default", "auto"}
	now := time.Now()
	se.usage["work"] = usagebudget.Reading{Percentage: 60, FiveHour: win(60, now)}
	se.usage["third"] = usagebudget.Reading{Percentage: 5, FiveHour: win(5, now)}
	se.tickN(2)
	ov, _, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.Equal(t, ov.Account, "third")
}

func TestMoveAccount_ManualAndRefusals(t *testing.T) {
	se := newSwitchEnv(t)
	se.usage["default"] = usagebudget.Reading{}
	res, err := se.m.MoveAccount("work", false)
	testutil.NoError(t, err)
	testutil.Equal(t, res.To, "work")
	testutil.True(t, se.moved())
	ov, _, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.Equal(t, ov.By, db.OverrideByManual)
	testutil.Equal(t, len(se.notif.calls), 0) // manual: no notice, no board item
	testutil.Equal(t, len(se.boardItems(t)), 0)

	se.h.idle = false
	_, err = se.m.MoveAccount("default", false)
	testutil.ErrorIs(t, err, agent.ErrAccountMoveBusy)

	se.cfg.Resident.Enabled = false
	_, err = se.m.MoveAccount("default", true)
	testutil.Contains(t, err.Error(), "not enabled")
}

// Solo parked on its weekly limit: five-hour at 0%, weekly at 100%, Claude
// Code's persistent wait footer on screen, output never quiet for 3s.
const limitWaitScreen = "You've hit your weekly limit \u00b7 resets Nov 17 at 12am (America/Los_Angeles)\r\n" +
	"\r\n\u26a0Usage limit reached \u00b7 limit resets Nov 17 at 12am\r\n" +
	"Continuing automatically at Nov 17 at 12am \u00b7 Esc to cancel\r\n" +
	"manual mode on\r\n"

func TestAutoSwitch_WeeklyLimitWaitFooterIsSwitchable(t *testing.T) {
	se := newSwitchEnv(t)
	now := time.Now()
	se.usage["default"] = usagebudget.Reading{
		FiveHour: win(0, now),
		SevenDay: &usagebudget.Window{Percentage: 100, ResetAt: now.Add(96 * time.Hour), LastProbedAt: now},
	}
	se.usage["work"] = usagebudget.Reading{SevenDay: &usagebudget.Window{Percentage: 3, ResetAt: now.Add(96 * time.Hour), LastProbedAt: now}}
	// Not raw-idle (the footer keeps repainting), but the screen is parked.
	se.h.idle = false
	se.h.tail = []byte(limitWaitScreen)
	clock := now
	se.m.now = func() time.Time { return clock }
	for i := 0; i < 8 && !se.moved(); i++ {
		clock = clock.Add(5 * time.Second)
		se.m.tick()
	}
	testutil.True(t, se.moved())
	// The move was forced past its raw-idle busy check, to the other account.
	ov, ok, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, ov.Account, "work")
	testutil.Equal(t, ov.By, db.OverrideByAuto)
}

// The `>` picker on the Solo tab works while Solo sits on the usage-limit wait
// screen: raw output never goes quiet, so the picker forces the move (the
// daemon refuses an unforced one as busy), and the pick is a manual hold.
func TestMoveAccount_ManualWorksOnLimitWaitScreen(t *testing.T) {
	se := newSwitchEnv(t)
	se.h.idle = false
	se.h.tail = []byte(limitWaitScreen)
	_, err := se.m.MoveAccount("work", false)
	testutil.ErrorIs(t, err, agent.ErrAccountMoveBusy)
	res, err := se.m.MoveAccount("work", true)
	testutil.NoError(t, err)
	testutil.Equal(t, res.To, "work")
	ov, ok, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, ov.By, db.OverrideByManual)
}

func TestAutoSwitch_WorkingScreenIsNotSwitchable(t *testing.T) {
	se := newSwitchEnv(t)
	se.h.idle = false
	se.h.tail = []byte("\u273b Thinking\u2026 (12s \u00b7 esc to interrupt)\r\n")
	clock := time.Now()
	se.m.now = func() time.Time { return clock }
	for i := 0; i < 8; i++ {
		clock = clock.Add(5 * time.Second)
		se.m.tick()
	}
	testutil.False(t, se.moved())
}

// A manual hold only protects the account the human picked.
func TestAutoSwitch_ManualHoldIsScopedToThePickedAccount(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	// The marker says "manual work", but the resident actually runs elsewhere (default): no hold.
	testutil.NoError(t, se.db.SetMeta(se.task.ID, db.ResidentMetaNamespace, db.MetaKeyAccountOverride,
		`{"account":"work","by":"manual","at":"x"}`))
	se.cfg.Resident.Accounts = []string{"work"} // default is no longer allowed
	old := time.Now().Add(-time.Hour).UTC().Format(db.ResidentTimeFormat)
	testutil.NoError(t, se.db.SetMeta(se.task.ID, db.ResidentMetaNamespace, db.MetaKeyLastAutoSwitch, old))
	se.tickN(2)
	testutil.True(t, se.moved()) // moved off the delisted account despite a manual marker for another account
}

// Picking auto in the picker follows headroom: no hold, no cooldown stamp.
func TestMoveAccount_AutoPickFollowsHeadroomWithoutHold(t *testing.T) {
	se := newSwitchEnv(t)
	se.usage["default"] = usagebudget.Reading{}
	_, err := se.m.MoveAccount("auto", false)
	testutil.NoError(t, err)
	ov, ok, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, ov.By, db.OverrideByFollow)
	testutil.True(t, se.db.ResidentLastAutoSwitch(se.task.ID).IsZero())
}

// Non-urgent moves wait for a quiet session; moving off a depleted account does not.
func TestAutoSwitch_NonUrgentMovesNeedNoPromptAndNoDraft(t *testing.T) {
	setup := func(t *testing.T) *switchEnv {
		se := newSwitchEnv(t)
		se.recovered("default")
		se.onWork(db.OverrideByAuto, time.Hour)
		se.cfg.Resident.Accounts = []string{"default"} // work is no longer listed
		return se
	}
	t.Run("pending prompt blocks an off-list move", func(t *testing.T) {
		se := setup(t)
		se.m.waiting = func(string) bool { return true }
		se.tickN(6)
		testutil.False(t, se.moved())
		se.m.waiting = func(string) bool { return false }
		se.tickN(2)
		testutil.True(t, se.moved())
	})
	t.Run("unsent draft blocks an off-list move", func(t *testing.T) {
		se := setup(t)
		draft := true
		se.m.draftFn = func(agent.SessionHandle) bool { return draft }
		se.tickN(6)
		testutil.False(t, se.moved())
		draft = false
		se.tickN(2)
		testutil.True(t, se.moved())
	})
	t.Run("depleted move ignores prompt and draft", func(t *testing.T) {
		se := newSwitchEnv(t) // default depleted
		se.m.waiting = func(string) bool { return true }
		se.m.draftFn = func(agent.SessionHandle) bool { return true }
		se.tickN(2)
		testutil.True(t, se.moved())
	})
}

// The all-depleted board item is posted once per episode even across a daemon
// bounce (a new manager over the same database).
func TestAutoSwitch_NoneUsableItemSurvivesManagerRestart(t *testing.T) {
	se := newSwitchEnv(t)
	se.usage["work"] = usagebudget.Reading{Percentage: 100, FiveHour: win(100, time.Now())}
	se.tickN(4)
	testutil.Equal(t, len(se.boardItems(t)), 1)

	// Daemon bounce: a fresh manager, same DB and runner.
	m2 := newResidentManager(se.db, se.m.runner, func() config.Config { return se.cfg })
	m2.mover = se.m.mover
	m2.notifier = se.m.notifier
	m2.now = time.Now
	for range 4 {
		m2.tick()
	}
	testutil.Equal(t, len(se.boardItems(t)), 1)
	testutil.False(t, se.moved())

	// Recovery ends the episode; a later depletion posts again.
	se.recovered("work")
	se.tickN(2)
	testutil.True(t, se.moved())
}

// MoveAccount runs on the RPC goroutine while tick runs on the manager loop.
func TestAutoSwitch_TickAndMoveAccountDoNotRace(t *testing.T) {
	se := newSwitchEnv(t)
	se.usage["default"] = usagebudget.Reading{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20 {
			_, _ = se.m.MoveAccount("work", true)
		}
	}()
	for range 20 {
		se.m.tick()
	}
	<-done
}

// The resident's stored account vanished (removed from config.toml): BuildCmd would
// refuse every spawn, so the start recovers onto the list's next valid account.
func TestStart_RemovedAccountRecoversOntoNextListedAccount(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	testutil.NoError(t, se.db.SetResidentAccountMove(se.task.ID, "work", se.task.SessionID,
		db.ResidentAccountOverride{Account: "work", By: db.OverrideByManual}))
	delete(se.cfg.Accounts, "work")
	se.cfg.Resident.Accounts = []string{"work", "default"}

	got := se.restart()
	testutil.Equal(t, got.task.Account, "") // default, the next valid listed account
	row, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, row.Account, "")
	ov, ok, _ := se.db.ResidentAccountOverride(se.task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, ov.By, db.OverrideByAuto)
	testutil.Equal(t, ov.Account, "default")
	testutil.False(t, se.db.ResidentLastAutoSwitch(se.task.ID).IsZero()) // cooldown stamped
	// The old account's storage is unresolvable, so the conversation cannot be
	// copied: Solo starts fresh and is told so; the board shows it.
	testutil.Equal(t, got.resume, false)
	testutil.True(t, got.task.SessionID != "" && got.task.SessionID != se.task.SessionID)
	testutil.Equal(t, len(se.notif.calls), 1)
	testutil.Contains(t, se.notif.calls[0].text, "could not be carried over")
	items := se.boardItems(t)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Kind, "fyi")
	testutil.Contains(t, items[0].Body, "NOT be carried over")
	// A later restart is an ordinary one: no further recovery.
	again := se.restart()
	testutil.Equal(t, again.task.Account, "")
	testutil.Equal(t, len(se.notif.calls), 1)
}

// An account that loses its Claude login directory (still configured) is just as
// unusable for BuildCmd, and the recovery lands on a valid listed account.
func TestStart_AccountWithoutClaudeDirRecovers(t *testing.T) {
	se := newSwitchEnv(t)
	se.recovered("default")
	testutil.NoError(t, se.db.SetResidentAccountMove(se.task.ID, "work", se.task.SessionID,
		db.ResidentAccountOverride{Account: "work", By: db.OverrideByAuto}))
	se.cfg.Accounts["work"] = config.Account{CodexHome: t.TempDir()}
	got := se.restart()
	testutil.Equal(t, got.task.Account, "")
}

func TestResidentAccountUsable(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Accounts = map[string]config.Account{"ok": {ClaudeConfigDir: "/x"}, "cx": {CodexHome: "/y"}}
	testutil.True(t, residentAccountUsable(cfg, "claude", ""))
	testutil.True(t, residentAccountUsable(cfg, "claude", "ok"))
	testutil.False(t, residentAccountUsable(cfg, "claude", "cx"))
	testutil.False(t, residentAccountUsable(cfg, "claude", "ghost"))
	testutil.False(t, residentAccountUsable(cfg, "no-such-backend", ""))
}
