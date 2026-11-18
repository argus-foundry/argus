package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/hera"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/gdamore/tcell/v2"
)

func TestSmoke_ResidentApprovalDecision(t *testing.T) {
	for _, approve := range []bool{true, false} {
		t.Run(map[bool]string{true: "approve", false: "deny"}[approve], func(t *testing.T) {
			a, d := residentTestApp(t)
			r, err := d.ResidentTask()
			testutil.NoError(t, err)
			call := db.ApprovalCall{TaskID: r.ID, Session: "session", Cwd: r.Worktree, Tool: "Bash", Input: json.RawMessage(`{"command":"echo probe"}`)}
			launch := agent.ResidentApprovalLaunch{Token: "test-private", Generation: agent.HashResidentMCPToken("test-private")}
			// The grant must match the active private file's public generation.
			call.Launch = launch.Generation
			item, _, err := d.RequestResidentApproval(call, time.Now())
			testutil.NoError(t, err)
			raw, _ := json.Marshal(launch)
			testutil.NoError(t, os.MkdirAll(filepath.Dir(agent.ResidentApprovalLaunchPath()), 0o700))
			testutil.NoError(t, os.WriteFile(agent.ResidentApprovalLaunchPath(), raw, 0o600))
			sim, stop := wireApp(t, a)
			defer stop()
			sim.SetSize(180, 45)
			readUI(t, a.tapp, func() {
				a.switchTab(widget.TabResident)
				a.heraPage.Machine().SetRegion(hera.FocusAgent)
				a.confirmResidentApproval(d, &db.HeraBoardItem{ID: item.BoardID, UpdatedAt: item.CreatedAt, ResidentApproval: true}, approve)
			})
			sim.InjectKey(tcell.KeyEscape, 0, 0)
			syncUI(t, a.tapp)
			_, allow, err := d.RequestResidentApproval(call, time.Now())
			testutil.NoError(t, err)
			testutil.False(t, allow)
			readUI(t, a.tapp, func() {
				a.confirmResidentApproval(d, &db.HeraBoardItem{ID: item.BoardID, UpdatedAt: item.CreatedAt, ResidentApproval: true}, approve)
			})
			sim.InjectKey(tcell.KeyRune, 'y', 0)
			waitForCond(t, a.tapp, "approval notice committed", func() bool { notices, err := d.PendingHeraBoardNotices(); return err == nil && len(notices) == 1 })
			_, allow, err = d.RequestResidentApproval(call, time.Now())
			testutil.NoError(t, err)
			testutil.Equal(t, allow, approve)
		})
	}
}

// A second approve on an already-approved item (a stale board snapshot) says
// what already happened instead of a generic failure, and sends nothing new.
func TestSmoke_ResidentApprovalReclickExplainsState(t *testing.T) {
	a, d := residentTestApp(t)
	r, err := d.ResidentTask()
	testutil.NoError(t, err)
	launch := agent.ResidentApprovalLaunch{Token: "test-private", Generation: agent.HashResidentMCPToken("test-private")}
	call := db.ApprovalCall{TaskID: r.ID, Session: "session", Cwd: r.Worktree, Tool: "Bash", Input: json.RawMessage(`{"command":"echo probe"}`), Launch: launch.Generation}
	item, _, err := d.RequestResidentApproval(call, time.Now())
	testutil.NoError(t, err)
	raw, _ := json.Marshal(launch)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(agent.ResidentApprovalLaunchPath()), 0o700))
	testutil.NoError(t, os.WriteFile(agent.ResidentApprovalLaunchPath(), raw, 0o600))
	testutil.NoError(t, d.DecideResidentApproval(item.BoardID, item.CreatedAt, launch.Generation, true, time.Now()))
	sim, stop := wireApp(t, a)
	defer stop()
	sim.SetSize(180, 45)
	readUI(t, a.tapp, func() {
		a.switchTab(widget.TabResident)
		a.confirmResidentApproval(d, &db.HeraBoardItem{ID: item.BoardID, UpdatedAt: item.CreatedAt, ResidentApproval: true, ApprovalState: db.ApprovalPending}, true)
	})
	sim.InjectKey(tcell.KeyRune, 'y', 0)
	waitForCond(t, a.tapp, "state error shown", func() bool { return a.statusbar.Error() != "" })
	readUI(t, a.tapp, func() {
		testutil.Contains(t, a.statusbar.Error(), "Not applied: deferred call #")
		testutil.Contains(t, a.statusbar.Error(), "already approved at")
		testutil.Contains(t, a.statusbar.Error(), "waiting for the agent to run it")
	})
	notices, err := d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(notices), 1)
}
