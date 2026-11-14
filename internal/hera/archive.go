package hera

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// maxFyiBody mirrors the board body cap (db.validBoardContent: 64 KiB).
const maxFyiBody = 64 * 1024

// ArchiveStore is the narrow DB surface the done-done predicate and
// ArchiveRole need (add-hera-auto-archive). Satisfied by *db.DB.
type ArchiveStore interface {
	Get(taskID string) (*model.Task, error)
	HeraRole(id int64) (*db.HeraRole, error)
	HeraLiveBindingByRole(roleID int64) (*db.HeraBinding, error)
	HeraRoleStatusFor(roleID int64) (*db.HeraRoleStatus, error)
	HeraInbox(roleID int64) ([]*db.HeraMessage, error)
	HeraUnreadFromRole(roleID int64) (int, error)
	ListHeraLiveBindingsByTask(taskID string) ([]*db.HeraBinding, error)
	ListHeraBlocks(orchID int64) ([]db.HeraBlock, error)
	ListHeraRoles(orchID int64, includeArchived bool) ([]*db.HeraRole, error)
	ListHeraRolesByKind(orchID int64, kind db.HeraRoleKind) ([]*db.HeraRole, error)
	ListHeraOrchLinks() ([]db.HeraOrchLink, error)
	HeraBridgedChildOrchs(taskID string, parentOrchID int64) ([]int64, error)
	EndHeraCoordinatorBindingsForTask(taskID, reason string) (int, error)
	ListHeraBoardItems(roleID int64, includeResolved bool) ([]*db.HeraBoardItem, error)
	PostHeraBoardItem(roleID int64, kind, subject, body string) (*db.HeraBoardItem, error)
	ResolveHeraBoardItem(roleID, id int64, by, note string) error
	IsResidentWorker(taskID string) (bool, error)
	SetArchived(id string, archived bool) error
	ArchiveHeraRoleAs(id int64, actor db.HeraActor) error
}

// ArchiveRunner is the daemon-side session seam ArchiveRole uses; injected so
// the gating is testable without a PTY. Stop must be best-effort-safe.
type ArchiveRunner interface {
	IsAlive(taskID string) bool
	IsIdle(taskID string) bool
	BlockedOnPrompt(taskID string) bool
	Stop(taskID string) error
}

// GitCheck reports whether the task's git work is confirmed landed with a
// clean worktree. ok=false carries a human reason; an error or unknown MUST be
// returned as ok=false (fail closed). The production implementation combines
// the merge-safety classifier with a dirty-file count.
type GitCheck func(t *model.Task) (ok bool, reason string)

// ArchiveVerdict is the done-done answer for one role.
type ArchiveVerdict struct {
	Archivable bool
	Reason     string // first failing clause; empty when Archivable
}

func refuse(format string, a ...any) ArchiveVerdict {
	return ArchiveVerdict{Reason: fmt.Sprintf(format, a...)}
}

// ArchivableVerdict evaluates the done-done predicate (design D1) for role,
// failing closed: any lookup error refuses. A nil git check is treated as
// "cannot confirm" for any task that has a branch or worktree.
func ArchivableVerdict(store ArchiveStore, git GitCheck, role *db.HeraRole) ArchiveVerdict {
	if role.Kind == db.HeraKindCoordinator {
		return refuse("coordinator roles are archived by the human or the parent coordinator, not hera_archive")
	}
	if role.PinnedAt != nil {
		return refuse("role is pinned")
	}
	binding, err := store.HeraLiveBindingByRole(role.ID)
	if err != nil {
		return refuse("no live binding (%v)", err)
	}
	task, err := store.Get(binding.ArgusTaskID)
	if err != nil || task == nil {
		return refuse("task %s not loadable (%v)", binding.ArgusTaskID, err)
	}
	if task.IsResident() || task.Pinned {
		return refuse("task is resident or pinned")
	}
	if res, err := store.IsResidentWorker(task.ID); err != nil || res {
		return refuse("standing-team member or membership unverifiable")
	}
	if task.Status != model.StatusComplete {
		return refuse("task status is %s, not complete", task.Status)
	}
	// A missing status row means "never reported", not blocked; any other
	// lookup error cannot be confirmed and refuses.
	if st, err := store.HeraRoleStatusFor(role.ID); err != nil && !errors.Is(err, db.ErrHeraNotFound) {
		return refuse("role status unreadable (%v)", err)
	} else if err == nil && st != nil && st.Status == db.HeraStatusBlocked {
		return refuse("role is blocked (needs input)")
	}
	// SetArchived is task-wide: another live role on the same task would lose
	// its state while staying live (multi-orchestrator join, or a sub-
	// coordinator's un-ended coordinator hat).
	bindings, err := store.ListHeraLiveBindingsByTask(task.ID)
	if err != nil {
		return refuse("task bindings unreadable (%v)", err)
	}
	kids, err := childOrchs(store, role, task.ID)
	if err != nil {
		return refuse("orchestrator links unreadable (%v)", err)
	}
	bridged := map[int64]bool{}
	for _, k := range kids {
		bridged[k] = true
	}
	for _, b := range bindings {
		// The sub-coordinator's own coordinator hat in a bridged child
		// orchestrator is part of the same unit (children are verified below).
		if b.RoleID == role.ID {
			continue
		}
		if bridged[b.OrchestratorID] {
			if br, err := store.HeraRole(b.RoleID); err == nil && br.Kind == db.HeraKindCoordinator {
				continue
			}
		}
		return refuse("task is also bound to another live role (%d)", b.RoleID)
	}
	if msgs, err := store.HeraInbox(role.ID); err != nil {
		return refuse("inbox unreadable (%v)", err)
	} else if len(msgs) > 0 {
		return refuse("%d unread hera message(s) to this role", len(msgs))
	}
	if n, err := store.HeraUnreadFromRole(role.ID); err != nil {
		return refuse("outbox unreadable (%v)", err)
	} else if n > 0 {
		return refuse("%d message(s) from this role not yet read by their recipient", n)
	}
	if v := dependentsVerdict(store, role); !v.Archivable && v.Reason != "" {
		return v
	}
	if v := childrenVerdict(store, role, task.ID); !v.Archivable && v.Reason != "" {
		return v
	}
	if task.Branch != "" || task.Worktree != "" {
		if git == nil {
			return refuse("git state cannot be confirmed")
		}
		if ok, reason := git(task); !ok {
			return refuse("git work not confirmed landed: %s", reason)
		}
	}
	return ArchiveVerdict{Archivable: true}
}

// dependentsVerdict refuses while a not-yet-done plan node is blocked by role.
// The zero ArchiveVerdict (Reason "") means "no objection".
func dependentsVerdict(store ArchiveStore, role *db.HeraRole) ArchiveVerdict {
	blocks, err := store.ListHeraBlocks(role.OrchestratorID)
	if err != nil {
		return refuse("plan edges unreadable (%v)", err)
	}
	for _, b := range blocks {
		if b.BlockerRoleID != role.ID {
			continue
		}
		dep, err := store.HeraRole(b.BlockedRoleID)
		if err != nil {
			return refuse("dependent role %d unreadable (%v)", b.BlockedRoleID, err)
		}
		if dep.CancelledAt != nil || dep.NukedAt != nil {
			continue
		}
		if st, err := store.HeraRoleStatusFor(dep.ID); err == nil && st != nil && st.Status == db.HeraStatusDone {
			continue
		}
		return refuse("plan node %q still depends on this role", dep.Name)
	}
	return ArchiveVerdict{}
}

// childOrchs returns the orchestrators this role bridges to (sub-coordinator):
// explicit hera_orch_links rows (TUI adopt/re-parent) PLUS child orchestrators
// whose coordinator hat shares the role's task (plan-DAG kind=subcoord, which
// writes no link row).
func childOrchs(store ArchiveStore, role *db.HeraRole, taskID string) ([]int64, error) {
	links, err := store.ListHeraOrchLinks()
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	var out []int64
	for _, l := range links {
		if l.ParentRoleID == role.ID && !seen[l.ChildOrchestratorID] {
			seen[l.ChildOrchestratorID] = true
			out = append(out, l.ChildOrchestratorID)
		}
	}
	if taskID != "" {
		bridged, err := store.HeraBridgedChildOrchs(taskID, role.OrchestratorID)
		if err != nil {
			return nil, err
		}
		for _, id := range bridged {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out, nil
}

// childrenVerdict applies the sub-coordinator clause: every non-coordinator
// child role archived and no open `decision` board item on the child
// coordinator(s). Open `fyi` items do not block (ArchiveRole pushes them up).
func childrenVerdict(store ArchiveStore, role *db.HeraRole, taskID string) ArchiveVerdict {
	kids, err := childOrchs(store, role, taskID)
	if err != nil {
		return refuse("orchestrator links unreadable (%v)", err)
	}
	for _, oid := range kids {
		roles, err := store.ListHeraRoles(oid, false)
		if err != nil {
			return refuse("child roles unreadable (%v)", err)
		}
		for _, cr := range roles {
			if cr.NukedAt != nil || cr.CancelledAt != nil {
				continue
			}
			if cr.Kind != db.HeraKindCoordinator {
				return refuse("child role %q is not archived", cr.Name)
			}
			items, err := store.ListHeraBoardItems(cr.ID, false)
			if err != nil {
				return refuse("board unreadable (%v)", err)
			}
			for _, it := range items {
				if it.Kind == "decision" && !it.Resolved {
					return refuse("open decision board item %q", it.Subject)
				}
			}
		}
	}
	return ArchiveVerdict{}
}

// pushFyiUp re-posts a sub-coordinator's open fyi board items to the parent
// orchestrator's coordinator board and resolves the originals.
func pushFyiUp(store ArchiveStore, role *db.HeraRole, taskID string) error {
	kids, err := childOrchs(store, role, taskID)
	if err != nil {
		return err
	}
	parents, err := store.ListHeraRolesByKind(role.OrchestratorID, db.HeraKindCoordinator)
	if err != nil {
		return err
	}
	var errs []error
	for _, oid := range kids {
		roles, err := store.ListHeraRoles(oid, false)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, cr := range roles {
			if cr.Kind != db.HeraKindCoordinator {
				continue
			}
			items, err := store.ListHeraBoardItems(cr.ID, false)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, it := range items {
				if it.Kind != "fyi" || it.Resolved {
					continue
				}
				if len(parents) == 0 {
					errs = append(errs, errors.New("no parent coordinator to receive fyi item"))
					continue
				}
				subject := "[" + role.Name + "] " + it.Subject
				for len(subject) > 240 {
					_, size := utf8.DecodeLastRuneInString(subject)
					subject = subject[:len(subject)-size]
				}
				// Idempotent across retries: a previous attempt that posted but
				// failed to resolve the original must not post a duplicate.
				marker := fmt.Sprintf("— moved from %s fyi #%d", role.Name, it.ID)
				posted, err := alreadyPosted(store, parents[0].ID, marker)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if !posted {
					body := it.Body
					for len(body) > maxFyiBody-len(marker)-2 {
						_, size := utf8.DecodeLastRuneInString(body)
						body = body[:len(body)-size]
					}
					if _, err := store.PostHeraBoardItem(parents[0].ID, "fyi", subject, body+"\n\n"+marker); err != nil {
						errs = append(errs, err)
						continue
					}
				}
				if err := store.ResolveHeraBoardItem(cr.ID, it.ID, "agent", "pushed to parent coordinator on archive"); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return errors.Join(errs...)
}

func alreadyPosted(store ArchiveStore, roleID int64, marker string) (bool, error) {
	items, err := store.ListHeraBoardItems(roleID, true)
	if err != nil {
		return false, err
	}
	for _, it := range items {
		if strings.HasSuffix(it.Body, marker) {
			return true, nil
		}
	}
	return false, nil
}

// ArchiveRoleResult is one role's outcome.
type ArchiveRoleResult struct {
	Role     string
	Archived bool
	Reason   string // refusal reason (or soft warning on Archived)
}

// ArchiveRole archives one role if (and only if) it is done-done. dryRun
// evaluates without writing. A live session must be idle and not parked at a
// prompt; it is stopped AFTER the archive write so a stop-triggered exit hook
// cannot race the status. Worktree and branch are never touched.
func ArchiveRole(store ArchiveStore, runner ArchiveRunner, git GitCheck, role *db.HeraRole, dryRun bool) ArchiveRoleResult {
	res := ArchiveRoleResult{Role: role.Name}
	if role.ArchivedAt != nil {
		res.Archived = true
		res.Reason = "already archived"
		return res
	}
	v := ArchivableVerdict(store, git, role)
	if !v.Archivable {
		res.Reason = v.Reason
		uxlog.Log("[hera] archive refused role=%s: %s", role.Name, v.Reason)
		return res
	}
	binding, _ := store.HeraLiveBindingByRole(role.ID)
	taskID := binding.ArgusTaskID
	alive := runner != nil && runner.IsAlive(taskID)
	if alive && (!runner.IsIdle(taskID) || runner.BlockedOnPrompt(taskID)) {
		res.Reason = "session is busy or parked at a prompt"
		uxlog.Log("[hera] archive refused role=%s: %s", role.Name, res.Reason)
		return res
	}
	if dryRun {
		res.Archived = true
		res.Reason = "dry run: archivable"
		return res
	}
	if err := pushFyiUp(store, role, taskID); err != nil {
		res.Reason = fmt.Sprintf("could not push fyi items to parent: %v", err)
		uxlog.Log("[hera] archive refused role=%s: %s", role.Name, res.Reason)
		return res
	}
	if err := store.SetArchived(taskID, true); err != nil {
		res.Reason = fmt.Sprintf("archive task: %v", err)
		uxlog.Log("[hera] archive refused role=%s: %s", role.Name, res.Reason)
		return res
	}
	if _, err := store.EndHeraCoordinatorBindingsForTask(taskID, db.HeraEndReasonAccepted); err != nil {
		uxlog.Log("[hera] archive role=%s: end coordinator hat failed: %v", role.Name, err)
	}
	if err := store.ArchiveHeraRoleAs(role.ID, db.HeraActorHuman); err != nil {
		res.Reason = fmt.Sprintf("task archived but hiding role failed: %v", err)
		res.Archived = true
		uxlog.Log("[hera] archive role=%s: %s", role.Name, res.Reason)
		return res
	}
	res.Archived = true
	if alive {
		// The predicate ran earlier (git check can take seconds): re-confirm
		// the session is still idle right before the stop.
		if !runner.IsIdle(taskID) || runner.BlockedOnPrompt(taskID) {
			res.Reason = "archived; session became busy so it was left running"
			uxlog.Log("[hera] archive role=%s: %s", role.Name, res.Reason)
			return res
		}
		if err := runner.Stop(taskID); err != nil {
			res.Reason = fmt.Sprintf("archived; stopping session failed: %v", err)
		}
	}
	uxlog.Log("[hera] archive ok role=%s task=%s", role.Name, taskID)
	return res
}

// FormatArchiveResults renders results for the MCP tool.
func FormatArchiveResults(results []ArchiveRoleResult, dryRun bool) string {
	var archived, refused []string
	for _, r := range results {
		if r.Archived {
			s := r.Role
			if r.Reason != "" {
				s += " (" + r.Reason + ")"
			}
			archived = append(archived, s)
		} else {
			refused = append(refused, fmt.Sprintf("%s: %s", r.Role, r.Reason))
		}
	}
	var b strings.Builder
	label := "archived"
	if dryRun {
		label = "archivable (dry run)"
	}
	fmt.Fprintf(&b, "- **%s**: %s\n", label, joinOrNone(archived))
	fmt.Fprintf(&b, "- **refused**: %s\n", joinOrNone(refused))
	return b.String()
}

func joinOrNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, "; ")
}
