package daemon

import (
	"fmt"
	"sort"
	"strings"

	"github.com/argus-foundry/argus/internal/db"
)

// residentTeamSection reads durable membership, never the task_meta marker:
// archiving clears meta but deliberately leaves the binding live. These reads
// do not consume inbox messages, which must survive the resident's restart.
func (r *taskRecycleRunner) residentTeamSection(taskID string) string {
	bindings, err := r.database.ListHeraLiveBindingsByTask(taskID)
	if err != nil {
		residentLog("[resident] recycle team bindings failed task=%s: %v", taskID, err)
		return ""
	}
	var b strings.Builder
	for _, binding := range bindings {
		standing, err := r.database.IsResidentOrchestrator(binding.OrchestratorID)
		if err != nil || !standing {
			continue
		}
		roles, err := r.database.ListHeraRoles(binding.OrchestratorID, true)
		if err != nil {
			residentLog("[resident] recycle team roster failed: %v", err)
			continue
		}
		sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
		b.WriteString("\n## Standing team (snapshot; messages are still unread)\n\n")
		for _, role := range roles {
			if role.Kind != db.HeraKindWorker {
				continue
			}
			wb, err := r.database.HeraLiveBindingByRole(role.ID)
			if err != nil {
				continue
			} // graduated/deleted roles have no live worker
			worker, err := r.database.Get(wb.ArgusTaskID)
			if err != nil {
				continue
			}
			status := "unknown"
			if st, err := r.database.HeraRoleStatusFor(role.ID); err == nil && st != nil {
				status = string(st.Status)
			}
			last, unread, err := r.database.HeraWorkerMessageSummary(role.ID, binding.RoleID)
			if err != nil {
				residentLog("[resident] recycle worker messages failed role=%d: %v", role.ID, err)
			}
			if last == "" {
				last = "(none)"
			}
			blocked := status == string(db.HeraStatusBlocked)
			for _, id := range r.runner.NeedsInputIDs() {
				if id == worker.ID {
					blocked = true
				}
			}
			fmt.Fprintf(&b, "- %s: project=%s; status=%s; blocked=%t; unread=%d; last status: %s\n", role.Name, worker.Project, status, blocked, unread, last)
		}
	}
	residentLog("[resident] recycle team snapshot task=%s bytes=%d", taskID, b.Len())
	return b.String()
}

// runningWorkers counts live sessions, rather than worker slots, and remains
// available with the resident disabled or stopped by a prerequisite failure.
func (m *residentManager) runningWorkers() int {
	resident, err := m.db.ResidentTask()
	if err != nil || resident == nil {
		return 0
	}
	bindings, err := m.db.ListHeraLiveBindingsByTask(resident.ID)
	if err != nil {
		residentLog("[resident] running-worker bindings failed: %v", err)
		return 0
	}
	count := 0
	for _, b := range bindings {
		standing, err := m.db.IsResidentOrchestrator(b.OrchestratorID)
		if err != nil || !standing {
			continue
		}
		roles, err := m.db.ListHeraRoles(b.OrchestratorID, true)
		if err != nil {
			residentLog("[resident] running-worker roster failed: %v", err)
			continue
		}
		for _, role := range roles {
			if role.Kind != db.HeraKindWorker {
				continue
			}
			wb, err := m.db.HeraLiveBindingByRole(role.ID)
			if err != nil {
				continue
			}
			if sess := m.runner.Get(wb.ArgusTaskID); sess != nil && sess.Alive() {
				count++
			}
		}
	}
	return count
}
