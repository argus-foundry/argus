package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
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
	se := &switchEnv{resEnv: e, notif: &fakeResNotifier{}}
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

func TestStart_PersistedAccountKeptWhileAllowedAndNotDepleted(t *testing.T) {
	se := newSwitchEnv(t)
	se.usage["default"] = usagebudget.Reading{}
	testutil.NoError(t, se.db.SetResidentAccountMove(se.task.ID, "work", se.task.SessionID,
		db.ResidentAccountOverride{Account: "work", By: db.OverrideByAuto}))
	got := se.restart()
	testutil.Equal(t, got.task.Account, "work")
	testutil.Equal(t, got.resume, true)
}

func TestStart_ListChangeRepicksUnlessManual(t *testing.T) {
	se := newSwitchEnv(t)
	se.usage["default"] = usagebudget.Reading{}
	se.cfg.Resident.Accounts = []string{"default"}
	testutil.NoError(t, se.db.SetResidentAccountMove(se.task.ID, "work", se.task.SessionID,
		db.ResidentAccountOverride{Account: "work", By: db.OverrideByAuto}))
	testutil.Equal(t, se.restart().task.Account, "") // work not allowed: back to default

	// A manual move outside the list holds across the restart.
	testutil.NoError(t, se.db.SetResidentAccountMove(se.task.ID, "work", se.task.SessionID,
		db.ResidentAccountOverride{Account: "work", By: db.OverrideByManual}))
	testutil.Equal(t, se.restart().task.Account, "work")
}

func TestStart_SkipsDepletedAndMissingListedAccounts(t *testing.T) {
	se := newSwitchEnv(t)
	se.cfg.Resident.Accounts = []string{"ghost", "default", "work"}
	// default is depleted (setup), so the list lands on work; ghost is skipped.
	testutil.NoError(t, se.db.SetResidentAccountMove(se.task.ID, "default", se.task.SessionID,
		db.ResidentAccountOverride{Account: "default", By: db.OverrideByAuto}))
	testutil.Equal(t, se.restart().task.Account, "work")
}

func TestStart_FirstStartUsesListNotDefault(t *testing.T) {
	e := newResEnv(t)
	e.cfg.Accounts = map[string]config.Account{"personal": {ClaudeConfigDir: t.TempDir()}}
	e.cfg.Resident.Accounts = []string{"personal"}
	t.Cleanup(agent.SetAccountUsageSource(func(config.Config) map[string]usagebudget.Reading { return nil }))
	e.m.tick()
	testutil.Equal(t, e.run.starts[0].task.Account, "personal")
}

func TestAutoSwitch_MovesBackUpWhenPreferredRecovers(t *testing.T) {
	se := newSwitchEnv(t)
	// Resident currently on work (auto-moved earlier); default (preferred) recovered.
	se.usage["default"] = usagebudget.Reading{}
	testutil.NoError(t, se.db.SetResidentAccountMove(se.task.ID, "work", se.task.SessionID,
		db.ResidentAccountOverride{Account: "work", By: db.OverrideByAuto}))
	// Cooldown long past.
	old := time.Now().Add(-time.Hour).UTC().Format(db.ResidentTimeFormat)
	testutil.NoError(t, se.db.SetMeta(se.task.ID, db.ResidentMetaNamespace, db.MetaKeyLastAutoSwitch, old))
	se.tickN(1)
	testutil.False(t, se.moved())
	se.tickN(1)
	testutil.True(t, se.moved())
	row, _ := se.db.Get(se.task.ID)
	testutil.Equal(t, row.Account, "")
	testutil.Contains(t, se.notif.calls[len(se.notif.calls)-1].text, "back to default")
	testutil.Contains(t, se.notif.calls[len(se.notif.calls)-1].text, "Continue whatever you were doing")
}

func TestAutoSwitch_ManualChoiceNotMovedBackUp(t *testing.T) {
	se := newSwitchEnv(t)
	se.usage["default"] = usagebudget.Reading{}
	testutil.NoError(t, se.db.SetResidentAccountMove(se.task.ID, "work", se.task.SessionID,
		db.ResidentAccountOverride{Account: "work", By: db.OverrideByManual}))
	row, _ := se.db.Get(se.task.ID)
	se.task = row
	se.tickN(6)
	testutil.False(t, se.moved())
}

func TestAutoSwitch_CooldownAppliesToMoveBackUp(t *testing.T) {
	se := newSwitchEnv(t)
	se.usage["default"] = usagebudget.Reading{}
	testutil.NoError(t, se.db.SetResidentAccountMove(se.task.ID, "work", se.task.SessionID,
		db.ResidentAccountOverride{Account: "work", By: db.OverrideByAuto}))
	recent := time.Now().Add(-2 * time.Minute).UTC().Format(db.ResidentTimeFormat)
	testutil.NoError(t, se.db.SetMeta(se.task.ID, db.ResidentMetaNamespace, db.MetaKeyLastAutoSwitch, recent))
	se.tickN(5)
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
