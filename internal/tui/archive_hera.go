package tui

import (
	"errors"
	"net/http"
	"slices"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/apiclient"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// archiveForcer is the optional remote-store capability that archives with the
// human override (REST {"force":true,"reason":...}).
type archiveForcer interface {
	ForceArchive(id, reason string) error
}

// tuiArchiveRunner adapts the TUI's session provider to hera.ArchiveRunner for
// the local-mode archive verdict. A session parked at a prompt is not visible
// here, so BlockedOnPrompt is conservative-false; a non-forced archive still
// requires the session to be idle.
type tuiArchiveRunner struct{ p agent.SessionProvider }

func (r tuiArchiveRunner) IsAlive(id string) bool    { return r.p.HasSession(id) }
func (r tuiArchiveRunner) IsIdle(id string) bool     { return slices.Contains(r.p.Idle(), id) }
func (tuiArchiveRunner) BlockedOnPrompt(string) bool { return false }
func (r tuiArchiveRunner) Stop(id string) error      { return r.p.Stop(id) }

// archiveChecks builds the verdict's git/PR checks; a seam so tests never run
// real git or gh.
var archiveChecks = func(cfgFn func() config.Config) (hera.GitCheck, hera.HandoffCheck) {
	return hera.NewGitCheck(cfgFn), hera.NewHandoffCheck()
}

// archiveTask is the Tasks-tab archive toggle (add-hera-handoff-archive). Local
// mode runs the same hera verdict as hera_archive for a hera-bound task, OFF
// the UI thread (it can run git/gh), then archives role and task together or
// refuses with the reason and offers the human a force confirm. Remote mode
// goes through the REST endpoint, which runs the verdict server-side.
func (a *App) archiveTask(id, name string, archived bool) {
	ldb, local := a.db.(*db.DB)
	if !local {
		a.plainArchive(id, name, archived)
		return
	}
	if !archived {
		if handled, res := hera.UnarchiveTaskGuard(ldb, id); handled {
			if !res.Archived {
				uxlog.Log("[hera] tui unarchive refused task=%s: %s", id, res.Reason)
				a.flashNotice("Unarchive refused: " + res.Reason)
			}
			a.refreshTasksAsync()
			return
		}
		a.plainArchive(id, name, archived)
		return
	}
	// A pending graduation keeps its own refusal + abandon escape (plainArchive).
	if pending, err := ldb.GraduationPendingErr(id); err != nil || pending {
		a.plainArchive(id, name, archived)
		return
	}
	// Snapshot the runner on the UI thread: restartDaemon swaps a.runner there.
	go a.guardedArchive(ldb, a.runner, id, name, false, "")
}

// guardedArchive runs hera.ArchiveTaskGuard (git/gh capable, so never on the UI
// thread) and delivers the outcome on the UI thread.
func (a *App) guardedArchive(ldb *db.DB, runner agent.SessionProvider, id, name string, force bool, reason string) {
	cfgFn := ldb.Config
	git, handoff := archiveChecks(cfgFn)
	out, res := hera.ArchiveTaskGuard(ldb, tuiArchiveRunner{runner}, git, handoff, id, force, reason)
	a.tapp.QueueUpdateDraw(func() {
		switch out {
		case hera.GuardNotBound:
			a.plainArchive(id, name, true)
		case hera.GuardArchived:
			uxlog.Log("[hera] tui archive ok task=%s kind=%s", id, res.Kind)
			a.refreshTasksAsync()
		case hera.GuardRefused:
			uxlog.Log("[hera] tui archive refused task=%s: %s", id, res.Reason)
			a.flashNotice("Archive refused: " + res.Reason)
			a.refreshTasksAsync()
			a.offerForceArchive(id, name, res.Reason, func() { go a.guardedArchive(ldb, a.runner, id, name, true, "forced from TUI") })
		}
	})
}

// offerForceArchive asks the human whether to override a refused archive.
func (a *App) offerForceArchive(id, name, reason string, force func()) {
	a.heraConfirmReturnTasks = true
	a.openHeraConfirm("Archive refused: "+reason,
		"Force-archive \""+name+"\" anyway? The override is recorded with the reason \"forced from TUI\"; the worktree and branch are kept.",
		force)
}

// plainArchive is the ordinary (non-hera) archive toggle: a partial-column
// SetArchived, so local mode also drops the task's queued messages in one
// transaction and remote mode hits /api/tasks/{id}/archive, whose handler runs
// the same cleanup (and, for hera-bound tasks, the hera verdict).
func (a *App) plainArchive(id, name string, archived bool) {
	err := a.db.SetArchived(id, archived)
	switch {
	case graduationPendingErr(err):
		uxlog.Log("[resident] tui archive refused task=%s: graduation pending", id)
		a.flashNotice(db.ErrGraduationPending.Error())
		if archived {
			a.graduationPendingEscape(id, "archive", true, func() {
				if err := a.db.SetArchived(id, true); err != nil {
					a.statusbar.SetError("Archive failed: " + err.Error())
				}
				a.refreshTasksAsync()
			})
		}
	case errors.Is(err, db.ErrHeraRoleLive):
		a.flashNotice("Archive refused: a hera role is still live on this task")
	case errors.Is(err, db.ErrHeraReclaimInProgress):
		a.flashNotice("The task is being reclaimed")
	case !archived && remoteArchiveRefused(err):
		a.flashNotice(err.Error())
	case archived && remoteArchiveRefused(err):
		var ae *apiclient.Error
		_ = errors.As(err, &ae)
		uxlog.Log("[hera] tui remote archive refused task=%s: %s", id, ae.Message)
		a.flashNotice("Archive refused: " + ae.Message)
		if f, ok := a.db.(archiveForcer); ok {
			a.offerForceArchive(id, name, ae.Message, func() {
				if err := f.ForceArchive(id, "forced from TUI"); err != nil {
					a.statusbar.SetError("Force archive failed: " + err.Error())
				}
				a.refreshTasksAsync()
			})
		}
	} // other errors: best-effort; display is source of truth
	a.refreshTasksAsync()
}

// remoteArchiveRefused reports an apiclient 409 that is NOT a pending graduation.
func remoteArchiveRefused(err error) bool {
	var ae *apiclient.Error
	return errors.As(err, &ae) && ae.Status == http.StatusConflict
}
