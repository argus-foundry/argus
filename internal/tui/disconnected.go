package tui

import (
	"slices"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// sessionListHealthy reports whether the runner's last session-list read was
// trustworthy. Runners that cannot fail (the in-process one) have no such
// signal and are always healthy; the daemon client exposes SessionListHealthy.
func sessionListHealthy(r any) bool {
	if h, ok := r.(interface{ SessionListHealthy() bool }); ok {
		return h.SessionListHealthy()
	}
	return true
}

// updateDisconnected recomputes the disconnected-agent set (show-disconnected-
// agents D1) from this tick's daemon alive set and feeds the Hera rail. It MUST run exactly once per periodic tick (the grace counter is
// tick-based) on the tview goroutine, with a.tasks current. healthy=false (the
// session list could not be read, or runningIDs is stale) fails open.
func (a *App) updateDisconnected(runningIDs []string, healthy bool) {
	// A nil running list means "unknown", never "no sessions": every real
	// provider returns a non-nil (possibly empty) slice on success, and nil on
	// an RPC/HTTP error (client.Client, apiclient.Provider) or a startGen-stale
	// tick. This covers providers with no SessionListHealthy of their own (the
	// --remote REST provider), so a network blip can't mark everything ○.
	healthy = healthy && runningIDs != nil
	candidates := make([]string, 0, len(a.tasks))
	for _, t := range a.tasks {
		if t == nil || t.Archived {
			continue
		}
		if t.Status == model.StatusInProgress || t.Status == model.StatusInReview {
			candidates = append(candidates, t.ID)
		}
	}
	ids, next := agent.DisconnectedIDs(candidates, runningIDs, healthy, a.disconnectedTicks)
	a.disconnectedTicks = next
	slices.Sort(ids)
	if !slices.Equal(ids, a.disconnectedIDs) {
		uxlog.Log("[tui] disconnected agents: %d → %d (healthy=%v) ids=%v", len(a.disconnectedIDs), len(ids), healthy, ids)
	}
	a.disconnectedIDs = ids
	a.heraPage.SetDisconnected(ids)
}

// DisconnectedTaskIDs returns the task IDs whose session is not running as of
// the last tick — the reusable liveness predicate (e.g. for automatic wake-up
// after a supervisor restart). Tview goroutine only.
func (a *App) DisconnectedTaskIDs() []string { return slices.Clone(a.disconnectedIDs) }
