package daemon

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/db"
)

const residentNoticeBatch = 20

// deliverNotices turns queued human-action notices (nuke/hide/rename of one of
// the standing resident's workers by anyone but the resident) into one Hera
// message each, sent from the affected worker's role to the resident
// coordinator. The notices come from db choke points that may run in the TUI
// process, so the daemon is the only deliverer. The handoff is crash-safe:
// db.DeliverResidentNotice deletes the outbox row and inserts the message in
// one transaction, so a crash leaves either the queued row or the committed
// message, never neither or both; the doorbell rings after the commit.
func (m *residentManager) deliverNotices() {
	notices, err := m.db.PendingResidentNotices(residentNoticeBatch)
	if err != nil {
		residentLog("[resident] notice poll failed: %v", err)
		return
	}
	for _, n := range notices {
		from, to, body, tldr, ok := m.composeNotice(n)
		if !ok {
			_ = m.db.CompleteResidentNotice(n.ID)
			continue
		}
		commit := m.commitNotice
		if commit == nil {
			commit = m.db.DeliverResidentNotice
		}
		msg, err := commit(n.ID, from, to, body, tldr)
		if err != nil {
			if permanentNoticeError(err) {
				// A row that can never be sent must not wedge every later notice.
				residentLog("[resident] notice %d dropped (permanent): %v", n.ID, err)
				_ = m.db.CompleteResidentNotice(n.ID)
				continue
			}
			residentLog("[resident] notice %d delivery failed, will retry: %v", n.ID, err)
			return // one failure (inbox full, rate limit) ends the pass; the next tick retries in order
		}
		if msg != nil && m.ringNotice != nil {
			m.ringNotice(msg)
		}
		residentLog("[resident] notice %d delivered kind=%s worker_role=%d", n.ID, n.Kind, n.WorkerRoleID)
	}
}

// permanentNoticeError reports a failure no retry can fix.
func permanentNoticeError(err error) bool {
	for _, perm := range []error{
		db.ErrHeraMessageRecipientInvalid, db.ErrHeraMessageTldrTooLong, db.ErrHeraMessageTldrMultiline,
		db.ErrHeraMessageTldrRequired, db.ErrHeraMessageBodyTooLarge, db.ErrHeraMessageSelfSend,
		db.ErrHeraMessageInvalidReplyTo,
	} {
		if errors.Is(err, perm) {
			return true
		}
	}
	return false
}

// boundedTldr makes s a single-line TLDR of at most db.HeraMaxTldrLen bytes,
// cutting at a rune boundary with an ellipsis. The full text belongs in the body.
func boundedTldr(s string) string {
	s = strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
	if len(s) <= db.HeraMaxTldrLen {
		return s
	}
	const ell = "…"
	cut := db.HeraMaxTldrLen - len(ell)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ell
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
	return worker.ID, coord.ID, b.String(), boundedTldr(tldr), true
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
