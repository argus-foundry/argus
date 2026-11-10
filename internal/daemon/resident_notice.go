package daemon

import (
	"errors"
	"fmt"
	"strings"

	"github.com/argus-foundry/argus/internal/db"
)

const residentNoticeBatch = 20

// deliverNotices turns queued human-action notices (nuke/hide/rename of one of
// the standing resident's workers by anyone but the resident) into one Hera
// message each, sent from the affected worker's role to the resident
// coordinator. The notices come from db choke points that may run in the TUI
// process, so the daemon is the only deliverer. Each notice is claimed before
// the send and deleted after it; a retryable failure releases the claim.
func (m *residentManager) deliverNotices() {
	notices, err := m.db.PendingResidentNotices(residentNoticeBatch)
	if err != nil {
		residentLog("[resident] notice poll failed: %v", err)
		return
	}
	for _, n := range notices {
		claimed, err := m.db.ClaimResidentNotice(n.ID)
		if err != nil || !claimed {
			continue
		}
		from, to, body, tldr, ok := m.composeNotice(n)
		if !ok {
			_ = m.db.CompleteResidentNotice(n.ID)
			continue
		}
		send := m.sendNotice
		if send == nil {
			send = func(from, to int64, body, tldr string) error {
				_, err := m.db.SendHeraMessage(from, to, body, tldr, nil)
				return err
			}
		}
		if err := send(from, to, body, tldr); err != nil {
			if errors.Is(err, db.ErrHeraMessageRecipientInvalid) {
				residentLog("[resident] notice %d dropped: resident recipient unavailable: %v", n.ID, err)
				_ = m.db.CompleteResidentNotice(n.ID)
				continue
			}
			residentLog("[resident] notice %d delivery failed, will retry: %v", n.ID, err)
			_ = m.db.ReleaseResidentNotice(n.ID)
			return // one failure (inbox full, rate limit) ends the pass; the next tick retries in order
		}
		_ = m.db.CompleteResidentNotice(n.ID)
		residentLog("[resident] notice %d delivered kind=%s worker_role=%d", n.ID, n.Kind, n.WorkerRoleID)
	}
}

// composeNotice resolves the sender (the affected worker role) and recipient
// (the resident coordinator of the same orchestrator) and writes the message.
// ok is false when the notice no longer applies (role gone, no coordinator).
func (m *residentManager) composeNotice(n db.ResidentNotice) (from, to int64, body, tldr string, ok bool) {
	worker, err := m.db.HeraRole(n.WorkerRoleID)
	if err != nil || worker == nil {
		return 0, 0, "", "", false
	}
	roles, err := m.db.ListHeraRoles(worker.OrchestratorID, true)
	if err != nil {
		return 0, 0, "", "", false
	}
	var coord *db.HeraRole
	for _, r := range roles {
		if r.Kind == db.HeraKindCoordinator {
			coord = r
			break
		}
	}
	if coord == nil || coord.ID == worker.ID {
		return 0, 0, "", "", false
	}
	name := worker.Name
	var b strings.Builder
	switch n.Kind {
	case db.ResidentNoticeNuked:
		tldr = fmt.Sprintf("Worker %s was nuked by the human", name)
		fmt.Fprintf(&b, "The human nuked your worker %q.", name)
		if n.Detail != "" {
			fmt.Fprintf(&b, " (%s)", n.Detail)
		}
		b.WriteString(m.nukeSummary(worker))
		b.WriteString(" The role is gone; do not message it.")
	case db.ResidentNoticeHidden:
		tldr = fmt.Sprintf("Worker %s was hidden by the human", name)
		fmt.Fprintf(&b, "The human hid your worker %q: it is out of the active team and its session was stopped. Its worktree and branch are untouched.", name)
	case db.ResidentNoticeRenamed:
		tldr = fmt.Sprintf("Worker renamed: %s", n.Detail)
		fmt.Fprintf(&b, "The human renamed a worker: %s. Address it by the new name from now on.", n.Detail)
	default:
		return 0, 0, "", "", false
	}
	return worker.ID, coord.ID, b.String(), tldr, true
}

// nukeSummary is the cheap "what was unmerged" line for a nuke: the worker's
// branch (its work lives there) and task status. The worktree is already
// reclaimed, so nothing deeper is read here.
func (m *residentManager) nukeSummary(worker *db.HeraRole) string {
	binds, err := m.db.ListHeraBindingsByRole(worker.ID)
	if err != nil || len(binds) == 0 {
		return " Unmerged work: unknown (no task record)."
	}
	t, err := m.db.Get(binds[len(binds)-1].ArgusTaskID)
	if err != nil || t == nil {
		return " Unmerged work: unknown (task record is gone)."
	}
	if t.Branch == "" {
		return fmt.Sprintf(" Task status was %s; it had no branch.", t.Status)
	}
	return fmt.Sprintf(" Task status was %s; its work was on branch %s, which was not verified merged, so treat anything unpushed as lost.", t.Status, t.Branch)
}
