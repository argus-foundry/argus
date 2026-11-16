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
	HeraUnreadFromRole(roleID int64) (int, error)
	ListHeraLiveBindingsByTask(taskID string) ([]*db.HeraBinding, error)
	ListHeraBlocks(orchID int64) ([]db.HeraBlock, error)
	ListHeraRoles(orchID int64, includeArchived bool) ([]*db.HeraRole, error)
	ListHeraOrchLinks() ([]db.HeraOrchLink, error)
	HeraBridgedChildOrchs(taskID string, parentOrchID int64) ([]int64, error)
	ListHeraBoardItems(roleID int64, includeResolved bool) ([]*db.HeraBoardItem, error)
	IsResidentWorker(taskID string) (bool, error)
	ArchiveHeraUnit(in db.ArchiveUnitInput) (string, error)
	UnarchiveHeraUnit(in db.UnarchiveUnitInput) (string, error)
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

// HandoffInfo is what a successful HandoffCheck learned about the open PR.
type HandoffInfo struct {
	PRURL  string
	TipSHA string
}

// HandoffCheck reports whether the task's work is handed off: PR open and not
// failing CI, branch pushed with local tip == remote tip == PR head, clean
// worktree. ok=false carries a human reason; any error or unknown MUST be
// returned as ok=false (fail closed).
type HandoffCheck func(t *model.Task) (info HandoffInfo, ok bool, reason string)

// ArchivableVerdict evaluates the merged done-done predicate (design D1) for
// role, failing closed: any lookup error refuses. A nil git check is treated as
// "cannot confirm" for any task that has a branch or worktree.
func ArchivableVerdict(store ArchiveStore, git GitCheck, role *db.HeraRole) ArchiveVerdict {
	task, v := commonChecks(store, role, db.ArchiveKindMerged)
	if v.Reason != "" {
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

// HandoffVerdict evaluates the handed-off predicate: every non-git done-done
// clause (task in_review or complete) plus the HandoffCheck. A nil check, or a
// task with no branch (nothing to hand off), refuses.
func HandoffVerdict(store ArchiveStore, handoff HandoffCheck, role *db.HeraRole) (ArchiveVerdict, HandoffInfo) {
	task, v := commonChecks(store, role, db.ArchiveKindHandedOff)
	if v.Reason != "" {
		return v, HandoffInfo{}
	}
	if handoff == nil || task.Branch == "" {
		return refuse("no PR/branch to hand off"), HandoffInfo{}
	}
	info, ok, reason := handoff(task)
	if !ok {
		return refuse("not handed off: %s", reason), HandoffInfo{}
	}
	return ArchiveVerdict{Archivable: true}, info
}

// ForcedVerdict applies ONLY the hard refusals that survive force=true.
func ForcedVerdict(store ArchiveStore, role *db.HeraRole) ArchiveVerdict {
	if _, v := commonChecks(store, role, db.ArchiveKindForced); v.Reason != "" {
		return v
	}
	return ArchiveVerdict{Archivable: true}
}

// commonChecks is the shared non-git clause set; it MUST stay in lockstep with
// db.archiveUnitChecks (the transactional twin). A non-empty Reason refuses.
// Forced mode runs only the hard refusals (coordinator, pinned, resident,
// standing-team, other live role).
func commonChecks(store ArchiveStore, role *db.HeraRole, kind db.ArchiveKind) (*model.Task, ArchiveVerdict) {
	forced := kind == db.ArchiveKindForced
	if role.Kind == db.HeraKindCoordinator {
		return nil, refuse("coordinator roles are archived by the human or the parent coordinator, not hera_archive")
	}
	if role.PinnedAt != nil {
		return nil, refuse("role is pinned")
	}
	if role.ArchivedAt != nil || role.NukedAt != nil || role.CancelledAt != nil {
		return nil, refuse("role is already archived, nuked or cancelled")
	}
	binding, err := store.HeraLiveBindingByRole(role.ID)
	if err != nil {
		return nil, refuse("no live binding (%v)", err)
	}
	task, err := store.Get(binding.ArgusTaskID)
	if err != nil || task == nil {
		return nil, refuse("task %s not loadable (%v)", binding.ArgusTaskID, err)
	}
	if task.IsResident() || task.Pinned {
		return nil, refuse("task is resident or pinned")
	}
	if res, err := store.IsResidentWorker(task.ID); err != nil || res {
		return nil, refuse("standing-team member or membership unverifiable")
	}
	if !forced {
		switch {
		case kind == db.ArchiveKindHandedOff:
			if task.Status != model.StatusComplete && task.Status != model.StatusInReview {
				return nil, refuse("task status is %s, not in_review or complete", task.Status)
			}
		case task.Status != model.StatusComplete:
			return nil, refuse("task status is %s, not complete", task.Status)
		}
		// A missing status row means "never reported", not blocked; any other
		// lookup error cannot be confirmed and refuses.
		if st, err := store.HeraRoleStatusFor(role.ID); err != nil && !errors.Is(err, db.ErrHeraNotFound) {
			return nil, refuse("role status unreadable (%v)", err)
		} else if err == nil && st != nil && st.Status == db.HeraStatusBlocked {
			return nil, refuse("role is blocked (needs input)")
		}
	}
	// SetArchived is task-wide: another live role on the same task would lose
	// its state while staying live (multi-orchestrator join, or a sub-
	// coordinator's un-ended coordinator hat). Hard under force too.
	bindings, err := store.ListHeraLiveBindingsByTask(task.ID)
	if err != nil {
		return nil, refuse("task bindings unreadable (%v)", err)
	}
	kids, err := childOrchs(store, role, task.ID)
	if err != nil {
		return nil, refuse("orchestrator links unreadable (%v)", err)
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
		return nil, refuse("task is also bound to another live role (%d)", b.RoleID)
	}
	if forced {
		return task, ArchiveVerdict{}
	}
	// Unread mail TO the role does not block: the archive transaction forwards
	// it to the coordinator as one digest. Mail the role SENT must be read.
	if n, err := store.HeraUnreadFromRole(role.ID); err != nil {
		return nil, refuse("outbox unreadable (%v)", err)
	} else if n > 0 {
		return nil, refuse("%d message(s) from this role not yet read by their recipient", n)
	}
	if v := dependentsVerdict(store, role); !v.Archivable && v.Reason != "" {
		return nil, v
	}
	if v := childrenVerdict(store, role, task.ID); !v.Archivable && v.Reason != "" {
		return nil, v
	}
	return task, ArchiveVerdict{}
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

// ArchiveRoleResult is one role's outcome.
type ArchiveRoleResult struct {
	Role     string
	Archived bool
	Kind     db.ArchiveKind // how it was (or would be, on dry run) archived
	Reason   string         // refusal reason (or soft warning on Archived)
}

// ArchiveOptions carries the optional behaviors of ArchiveRoleWith.
type ArchiveOptions struct {
	DryRun bool
	// Handoff enables the handed-off kind when the merged predicate refuses.
	Handoff HandoffCheck
	// Force + Reason archive without the done-done checks (hard refusals stay).
	Force  bool
	Reason string
	// ActorRoleID is the coordinator performing the archive (0 = not a hera caller).
	ActorRoleID int64
	// OnDigest is called after commit with the forwarded mail digest, if any, so
	// the caller can ring the coordinator doorbell.
	OnDigest func(*db.HeraMessage)
}

// ArchiveRole archives one role if (and only if) it is done-done (merged).
// See ArchiveRoleWith for handed-off and forced archives.
func ArchiveRole(store ArchiveStore, runner ArchiveRunner, git GitCheck, role *db.HeraRole, dryRun bool) ArchiveRoleResult {
	return ArchiveRoleWith(store, runner, git, role, ArchiveOptions{DryRun: dryRun})
}

// ArchiveRoleWith archives one role as merged, handed_off (when opts.Handoff
// is set and the merged predicate refuses) or forced. dryRun evaluates without
// writing. A live session must be idle and not parked at a prompt (forced
// skips this); it is stopped AFTER the archive write so a stop-triggered exit
// hook cannot race the status. Worktree and branch are never touched.
func ArchiveRoleWith(store ArchiveStore, runner ArchiveRunner, git GitCheck, role *db.HeraRole, opts ArchiveOptions) ArchiveRoleResult {
	res := ArchiveRoleResult{Role: role.Name}
	if role.ArchivedAt != nil {
		res.Archived = true
		res.Reason = "already archived"
		return res
	}
	refused := func(reason string) ArchiveRoleResult {
		res.Reason = reason
		uxlog.Log("[hera] archive refused role=%s: %s", role.Name, reason)
		return res
	}
	var (
		kind db.ArchiveKind
		info HandoffInfo
	)
	if opts.Force {
		if strings.TrimSpace(opts.Reason) == "" {
			return refused("force requires a non-empty reason")
		}
		if v := ForcedVerdict(store, role); !v.Archivable {
			return refused(v.Reason)
		}
		kind = db.ArchiveKindForced
	} else if mv := ArchivableVerdict(store, git, role); mv.Archivable {
		kind = db.ArchiveKindMerged
	} else if opts.Handoff == nil {
		return refused(mv.Reason)
	} else if hv, hi := HandoffVerdict(store, opts.Handoff, role); hv.Archivable {
		kind, info = db.ArchiveKindHandedOff, hi
	} else {
		return refused(fmt.Sprintf("not merged (%s); %s", mv.Reason, hv.Reason))
	}
	res.Kind = kind
	binding, err := store.HeraLiveBindingByRole(role.ID)
	if err != nil {
		return refused(fmt.Sprintf("no live binding (%v)", err))
	}
	taskID := binding.ArgusTaskID
	alive := runner != nil && runner.IsAlive(taskID)
	busy := func() bool { return alive && !opts.Force && (!runner.IsIdle(taskID) || runner.BlockedOnPrompt(taskID)) }
	if busy() {
		return refused("session is busy or parked at a prompt")
	}
	if opts.DryRun {
		res.Archived = true
		res.Reason = "dry run: archivable"
		return res
	}
	// The predicate (and its possibly slow git/PR check) ran before this point.
	// Re-run the same check once more as the last step BEFORE the transaction
	// (a git subprocess must not run under the database lock), then do every DB
	// re-verification and write in ONE transaction (db.ArchiveHeraUnit); the
	// session idleness re-check is its PreCommit hook. Any failure rolls the
	// whole unit back, so a refusal never leaves a half-archived state.
	task, gerr := store.Get(taskID)
	if gerr != nil || task == nil {
		return refused(fmt.Sprintf("task %s not loadable (%v)", taskID, gerr))
	}
	switch kind {
	case db.ArchiveKindMerged:
		if task.Branch != "" || task.Worktree != "" {
			if git == nil {
				return refused("git state cannot be confirmed")
			}
			if ok, reason := git(task); !ok {
				return refused("git work not confirmed landed: " + reason)
			}
		}
	case db.ArchiveKindHandedOff:
		hi, ok, reason := opts.Handoff(task)
		if !ok {
			return refused("not handed off: " + reason)
		}
		info = hi
	}
	_, err = store.ArchiveHeraUnit(db.ArchiveUnitInput{
		RoleID: role.ID, Kind: kind, Reason: opts.Reason, PRURL: info.PRURL, TipSHA: info.TipSHA,
		ActorRoleID: opts.ActorRoleID,
		PreCommit: func(string) error {
			if busy() {
				return &db.ArchiveRefusedError{Reason: "session became busy or parked at a prompt"}
			}
			return nil
		},
		PostCommit: func(d *db.HeraMessage) {
			if d != nil && opts.OnDigest != nil {
				opts.OnDigest(d)
			}
		},
	})
	if err != nil {
		var ref *db.ArchiveRefusedError
		if errors.As(err, &ref) {
			return refused("changed during archive: " + ref.Reason)
		}
		return refused(fmt.Sprintf("archive rolled back: %v", err))
	}
	res.Archived = true
	if alive {
		if err := runner.Stop(taskID); err != nil {
			res.Reason = fmt.Sprintf("archived; stopping session failed: %v", err)
		}
	}
	uxlog.Log("[hera] archive ok role=%s task=%s kind=%s", role.Name, taskID, kind)
	return res
}

// UnarchiveRole brings an archived worker/freelance role back (hera_unarchive):
// task un-archived, role visible, task back to in_progress, open handoff record
// settled "handed back". It starts no session and checks no git state.
func UnarchiveRole(store ArchiveStore, role *db.HeraRole) ArchiveRoleResult {
	res := ArchiveRoleResult{Role: role.Name}
	if _, err := store.UnarchiveHeraUnit(db.UnarchiveUnitInput{RoleID: role.ID, Note: "handed back"}); err != nil {
		var ref *db.ArchiveRefusedError
		if errors.As(err, &ref) {
			res.Reason = ref.Reason
		} else {
			res.Reason = fmt.Sprintf("unarchive failed: %v", err)
		}
		uxlog.Log("[hera] unarchive refused role=%s: %s", role.Name, res.Reason)
		return res
	}
	res.Archived = true
	uxlog.Log("[hera] unarchive ok role=%s", role.Name)
	return res
}

// ArchiveRefusedHint is appended to a refusal surfaced through an argus
// (non-hera) archive so the caller learns the override exists.
const ArchiveRefusedHint = " To override, a coordinator can call hera_archive(role_name=..., force=true, reason=...)."

// TaskGuardOutcome reports what ArchiveTaskGuard did.
type TaskGuardOutcome int

const (
	// GuardNotBound: no live hera role is bound to the task; the caller archives it as a plain task.
	GuardNotBound TaskGuardOutcome = iota
	// GuardArchived: the verdict passed and the role AND task were archived together.
	GuardArchived
	// GuardRefused: the verdict refused; Reason says why.
	GuardRefused
)

// ArchiveTaskGuard is the single entry for argus-side task archives (MCP
// task_archive, REST, TUI): a task with a live visible worker/freelance role
// runs the same hera verdict and archives the role in the same transaction, or
// is refused with the same reason. A task bound to a live coordinator is
// refused (archive it from hera). Lookup errors fail closed.
func ArchiveTaskGuard(store ArchiveStore, runner ArchiveRunner, git GitCheck, handoff HandoffCheck, taskID string, force bool, reason string) (TaskGuardOutcome, ArchiveRoleResult) {
	bindings, err := store.ListHeraLiveBindingsByTask(taskID)
	if err != nil {
		return GuardRefused, ArchiveRoleResult{Reason: fmt.Sprintf("hera bindings unreadable (%v)", err)}
	}
	var target *db.HeraRole
	for _, b := range bindings {
		r, rerr := store.HeraRole(b.RoleID)
		if rerr != nil {
			return GuardRefused, ArchiveRoleResult{Reason: fmt.Sprintf("hera role %d unreadable (%v)", b.RoleID, rerr)}
		}
		if r.ArchivedAt != nil || r.NukedAt != nil || r.CancelledAt != nil {
			continue
		}
		if r.Kind == db.HeraKindCoordinator {
			// A sub-coordinator's coordinator hat is part of the worker role's unit;
			// a pure coordinator task is archived from hera.
			continue
		}
		if target == nil {
			target = r
		}
	}
	if target == nil {
		for _, b := range bindings {
			if r, rerr := store.HeraRole(b.RoleID); rerr == nil && r.Kind == db.HeraKindCoordinator && r.ArchivedAt == nil && r.NukedAt == nil && r.CancelledAt == nil {
				return GuardRefused, ArchiveRoleResult{Role: r.Name, Reason: "task belongs to a live coordinator role; coordinators are archived by the human or the parent coordinator from the Hera view"}
			}
		}
		return GuardNotBound, ArchiveRoleResult{}
	}
	res := ArchiveRoleWith(store, runner, git, target, ArchiveOptions{Handoff: handoff, Force: force, Reason: reason})
	if res.Archived {
		return GuardArchived, res
	}
	return GuardRefused, res
}

// FormatArchiveResults renders results for the MCP tool.
func FormatArchiveResults(results []ArchiveRoleResult, dryRun bool) string {
	var archived, refused []string
	for _, r := range results {
		if r.Archived {
			s := r.Role
			if r.Kind != "" {
				s += " [" + strings.ReplaceAll(string(r.Kind), "_", " ") + "]"
			}
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
