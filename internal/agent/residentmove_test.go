package agent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

func residentMoveFixture(t *testing.T) (*moveFixture, *model.Task) {
	t.Helper()
	f := newMoveFixture(t, false)
	res, _, err := f.d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	res.Backend, res.SessionID = "test", "sid-r"
	testutil.NoError(t, f.d.Update(res))
	writeTranscript(t, filepath.Join(f.home, ".claude"), res.Worktree, "sid-r", `{"type":"user","sessionId":"sid-r"}`+"\n")
	return f, res
}

func TestMoveResidentAccount_CarriesConversationAndStoresOverride(t *testing.T) {
	f, res := residentMoveFixture(t)
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}}

	task, out, err := MoveResidentAccount(f.d, r, AccountMoveInput{TaskID: res.ID, Account: "work"}, ResidentMoveOpts{By: db.OverrideByAuto})
	testutil.NoError(t, err)
	testutil.Equal(t, out, AccountMoveResult{From: "default", To: "work", Resumed: true, Started: true})
	testutil.Equal(t, task.Account, "work")
	main, _ := readTranscript(t, f.workDir, res.Worktree, "sid-r")
	testutil.Contains(t, main, "sid-r")
	testutil.NotNil(t, r.kicked)
	testutil.Equal(t, r.kicked.Account, "work")
	testutil.Equal(t, r.kicked.SessionID, "sid-r")

	got, _ := f.d.Get(res.ID)
	testutil.Equal(t, got.Account, "work")
	ov, ok, err := f.d.ResidentAccountOverride(res.ID)
	testutil.NoError(t, err)
	testutil.True(t, ok)
	testutil.Equal(t, ov.Account, "work")
	testutil.Equal(t, ov.By, db.OverrideByAuto)
	testutil.True(t, time.Since(f.d.ResidentLastAutoSwitch(res.ID)) < time.Minute)
}

func TestMoveResidentAccount_Refusals(t *testing.T) {
	f, res := residentMoveFixture(t)
	idle := &moveRunner{sess: &moveSession{idle: true, alive: true}}

	t.Run("backend move", func(t *testing.T) {
		_, _, err := MoveResidentAccount(f.d, idle, AccountMoveInput{TaskID: res.ID, Backend: "codex"}, ResidentMoveOpts{})
		testutil.ErrorIs(t, err, ErrAccount)
	})
	t.Run("ordinary task", func(t *testing.T) {
		_, _, err := MoveResidentAccount(f.d, idle, AccountMoveInput{TaskID: f.task.ID, Account: "work"}, ResidentMoveOpts{})
		testutil.ErrorIs(t, err, ErrAccount)
		testutil.Nil(t, idle.kicked)
	})
	t.Run("busy needs force", func(t *testing.T) {
		busy := &moveRunner{sess: &moveSession{idle: false, alive: true}}
		_, _, err := MoveResidentAccount(f.d, busy, AccountMoveInput{TaskID: res.ID, Account: "work"}, ResidentMoveOpts{})
		testutil.ErrorIs(t, err, ErrAccountMoveBusy)
		_, ok, _ := f.d.ResidentAccountOverride(res.ID)
		testutil.False(t, ok)
	})
	t.Run("generic move still refuses", func(t *testing.T) {
		_, _, err := MoveTaskAccount(f.d, idle, AccountMoveInput{TaskID: res.ID, Account: "work"})
		testutil.ErrorIs(t, err, db.ErrResidentProtected)
	})
}

func TestMoveResidentAccount_AutoSkipsDepletedAndRefusesWhenNone(t *testing.T) {
	f, res := residentMoveFixture(t)
	now := time.Now()
	depleted := usagebudget.Reading{FiveHour: &usagebudget.Window{Percentage: 100, ResetAt: now.Add(time.Hour), LastProbedAt: now}}
	stubAccountUsage(t, map[string]usagebudget.Reading{"work": depleted})
	r := &moveRunner{sess: &moveSession{idle: true, alive: true}}

	_, _, err := MoveResidentAccount(f.d, r, AccountMoveInput{TaskID: res.ID, Account: AutoMoveTarget}, ResidentMoveOpts{})
	testutil.ErrorIs(t, err, ErrAccount)
	testutil.Contains(t, err.Error(), "depleted")
	testutil.Nil(t, r.kicked)

	stubAccountUsage(t, map[string]usagebudget.Reading{"work": {FiveHour: &usagebudget.Window{Percentage: 30, ResetAt: now.Add(time.Hour), LastProbedAt: now}}})
	_, out, err := MoveResidentAccount(f.d, r, AccountMoveInput{TaskID: res.ID, Account: AutoMoveTarget}, ResidentMoveOpts{})
	testutil.NoError(t, err)
	testutil.Equal(t, out.To, "work")
}

func TestMoveResidentAccount_NoTranscriptStillSwitches(t *testing.T) {
	f := newMoveFixture(t, false)
	res, _, err := f.d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	res.Backend = "test"
	testutil.NoError(t, f.d.Update(res))
	r := &moveRunner{} // no live session
	task, out, err := MoveResidentAccount(f.d, r, AccountMoveInput{TaskID: res.ID, Account: "work"}, ResidentMoveOpts{})
	testutil.NoError(t, err)
	testutil.False(t, out.Resumed)
	testutil.False(t, out.Started)
	testutil.Equal(t, task.Account, "work")
	_, ok, _ := f.d.ResidentAccountOverride(res.ID)
	testutil.True(t, ok)
}

func TestAccountDepleted(t *testing.T) {
	now := time.Now()
	w := func(p float64, reset time.Duration, age time.Duration) *usagebudget.Window {
		return &usagebudget.Window{Percentage: p, ResetAt: now.Add(reset), LastProbedAt: now.Add(-age)}
	}
	tests := []struct {
		name string
		r    usagebudget.Reading
		want bool
	}{
		{"empty reading", usagebudget.Reading{}, false},
		{"five hour full", usagebudget.Reading{FiveHour: w(100, time.Hour, 0)}, true},
		{"weekly full", usagebudget.Reading{SevenDay: w(100, time.Hour, 0)}, true},
		{"under", usagebudget.Reading{FiveHour: w(99, time.Hour, 0), SevenDay: w(50, time.Hour, 0)}, false},
		{"already reset", usagebudget.Reading{FiveHour: w(100, -time.Minute, 0)}, false},
		{"stale", usagebudget.Reading{FiveHour: w(100, time.Hour, 24*time.Hour)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { testutil.Equal(t, AccountDepleted(tt.r, now), tt.want) })
	}
}
