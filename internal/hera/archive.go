package hera

import (
	"errors"
	"fmt"
	"strings"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// ArchiveStore is the narrow DB surface the done-done predicate and
// ArchiveRole need (add-hera-auto-archive). Satisfied by *db.DB.
type ArchiveStore interface {
	Get(taskID string) (*model.Task, error)
	HeraRole(id int64) (*db.HeraRole, error)
	HeraLiveBindingByRole(roleID int64) (*db.HeraBinding, error)
	HeraRoleStatusFor(roleID int64) (*db.HeraRoleStatus, error)
	HeraInbox(roleID int64) ([]*db.HeraMessage, error)
	ListHeraBlocks(orchID int64) ([]db.HeraBlock, error)
	ListHeraRoles(orchID int64, includeArchived bool) ([]*db.HeraRole, error)
	ListHeraRolesByKind(orchID int64, kind db.HeraRoleKind) ([]*db.HeraRole, error)
	ListHeraOrchLinks() ([]db.HeraOrchLink, error)
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
	if st, err := store.HeraRoleStatusFor(role.ID); err == nil && st != nil && st.Status == db.HeraStatusBlocked {
		return refuse("role is blocked (needs input)")
	}
	if msgs, err := store.HeraInbox(role.ID); err != nil {
		return refuse("inbox unreadable (%v)", err)
	} else if len(msgs) > 0 {
		return refuse("%d unread hera message(s)", len(msgs))
	}
	if v := dependentsVerdict(store, role); !v.Archivable && v.Reason != "" {
		return v
	}
	if v := childrenVerdict(store, role); !v.Archivable && v.Reason != "" {
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

// childOrchs returns the orchestrators this role bridges to (sub-coordinator).
func childOrchs(store ArchiveStore, roleID int64) ([]int64, error) {
	links, err := store.ListHeraOrchLinks()
	if err != nil {
		return nil, err
	}
	var out []int64
	for _, l := range links {
		if l.ParentRoleID == roleID {
			out = append(out, l.ChildOrchestratorID)
		}
	}
	return out, nil
}

// childrenVerdict applies the sub-coordinator clause: every non-coordinator
// child role archived and no open `decision` board item on the child
// coordinator(s). Open `fyi` items do not block (ArchiveRole pushes them up).
func childrenVerdict(store ArchiveStore, role *db.HeraRole) ArchiveVerdict {
	kids, err := childOrchs(store, role.ID)
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
func pushFyiUp(store ArchiveStore, role *db.HeraRole) error {
	kids, err := childOrchs(store, role.ID)
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
				if len(subject) > 240 {
					subject = subject[:240]
				}
				if _, err := store.PostHeraBoardItem(parents[0].ID, "fyi", subject, it.Body); err != nil {
					errs = append(errs, err)
					continue
				}
				if err := store.ResolveHeraBoardItem(cr.ID, it.ID, "agent", "pushed to parent coordinator on archive"); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return errors.Join(errs...)
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
	if err := pushFyiUp(store, role); err != nil {
		res.Reason = fmt.Sprintf("could not push fyi items to parent: %v", err)
		uxlog.Log("[hera] archive refused role=%s: %s", role.Name, res.Reason)
		return res
	}
	if err := store.SetArchived(taskID, true); err != nil {
		res.Reason = fmt.Sprintf("archive task: %v", err)
		uxlog.Log("[hera] archive refused role=%s: %s", role.Name, res.Reason)
		return res
	}
	if err := store.ArchiveHeraRoleAs(role.ID, db.HeraActorHuman); err != nil {
		res.Reason = fmt.Sprintf("task archived but hiding role failed: %v", err)
		res.Archived = true
		uxlog.Log("[hera] archive role=%s: %s", role.Name, res.Reason)
		return res
	}
	res.Archived = true
	if alive {
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
