package agent

import (
	"log/slog"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// ReconcileStaleSessions flips DB tasks stuck at InProgress to InReview.
//
// Called once at startup (daemon Serve, or in-process TUI fallback) after the
// runner is created but before any new sessions are started. The runner is
// empty at that point, so any InProgress row in the DB describes a session
// that died with the previous process — flip it to InReview so the user (TUI
// or PWA) can resume or discard it.
//
// InReview is the right state because we don't know whether the agent
// finished or crashed; letting the user decide via Resume is safer than
// silently marking Complete.
func ReconcileStaleSessions(database *db.DB) (int, error) {
	flipped, err := ReconcileStaleSessionsExcept(database, nil)
	return len(flipped), err
}

// ReconcileStaleSessionsExcept is the session-supervisor variant of
// ReconcileStaleSessions: it flips InProgress→InReview for every task EXCEPT
// those whose ID is in alive — the set the supervisor still reports running
// across a daemon bounce. Those tasks are RE-ATTACHED (their agents never died),
// so flipping them to InReview would be a false termination; they stay
// InProgress. It returns the IDs it actually flipped — the TRUE orphans (the
// supervisor lost them, e.g. it also restarted) — so the caller can post an
// ARGUS_BOUNCED signal to exactly those and not to the re-attached ones.
//
// A nil/empty alive set flips every InProgress task, making this exactly
// equivalent to the in-process ReconcileStaleSessions (which is implemented as
// a thin wrapper). InReview (never Complete) is still the right landing state
// for an orphan: an inferred absence has no observed exit, so the #707 rule
// "Complete only on an observed clean exit" holds.
func ReconcileStaleSessionsExcept(database *db.DB, alive map[string]bool) ([]string, error) {
	return reconcileSessions(database, alive, nil, false)
}

// ReconcileInterruptedSessions is supervisor-mode reconciliation. Prior liveness
// and live Hera bindings survive stale task statuses; plain stopped tasks do not
// qualify without evidence. The legacy in-process entry point stays status-only.
func ReconcileInterruptedSessions(database *db.DB, alive, previous map[string]bool) ([]string, error) {
	return reconcileSessions(database, alive, previous, true)
}

func reconcileSessions(database *db.DB, alive, previous map[string]bool, liveness bool) ([]string, error) {
	tasks, err := database.Tasks()
	if err != nil {
		return nil, err
	}
	var flipped []string
	for _, t := range tasks {
		if t.IsResident() {
			continue
		}
		candidate := t.Status == model.StatusInProgress
		if liveness {
			if t.Archived || t.Status == model.StatusComplete {
				continue
			}
			meta, err := database.ListMeta(t.ID, db.HeraMetaNamespace)
			if err != nil {
				return flipped, err
			}
			closing := false
			for _, kv := range meta {
				if kv.Key == db.HeraMetaKeyReadyToClose && kv.Value == "true" {
					closing = true
				}
			}
			if closing {
				uxlog.Log("[bounce-recovery] skip closing task=%s", t.ID)
				slog.Info("[bounce-recovery] skip closing task", "task", t.ID)
				continue
			}
			candidate = candidate || previous[t.ID]
			if !candidate && (t.Status == model.StatusPending || t.Status == model.StatusInReview) {
				bindings, err := database.ListHeraLiveBindingsByTask(t.ID)
				if err != nil {
					return flipped, err
				}
				candidate = len(bindings) > 0
			}
		}
		if !candidate {
			continue
		}
		if alive[t.ID] {
			continue // re-attached live supervisor session — leave InProgress
		}
		t.SetStatus(model.StatusInReview)
		if uerr := database.Update(t); uerr != nil {
			slog.Warn("reconcile: update failed", "task", t.ID, "name", t.Name, "err", uerr)
			continue
		}
		slog.Info("[bounce-recovery] reconciled interrupted session", "task", t.ID, "name", t.Name)
		flipped = append(flipped, t.ID)
	}
	return flipped, nil
}
