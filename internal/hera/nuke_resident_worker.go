package hera

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// residentDeleteLog writes a resident delete line to both the uxlog and slog:
// the delete runs in the daemon, which never calls uxlog.Init, so uxlog alone
// is a silent no-op there (same dual-write as internal/daemon's residentLog).
func residentDeleteLog(format string, args ...any) {
	uxlog.Log(format, args...)
	slog.Info(fmt.Sprintf(format, args...)) //nolint:gosec // G706: internal diagnostics, not a user-controlled log-forging surface
}

// NukeResidentWorker is the resident's hera_delete_worker variant of
// NukeSingleRoleAs (actor = resident). It finishes the ROLE-SCOPED local work
// synchronously — nuke the role, archive its task, stop that task's session,
// remove that task's own worktree and local branch, prune its row once no
// session remains — so the tool result can be returned without waiting on the
// network or on the fleet-wide ReconcileHeraReclaims, which the caller runs
// asynchronously. The returned followUp deletes the worker's branch on origin
// (its own timeout, outcome logged) and re-discovers stacked branches off the
// deleted task's branch (network classification, the one per-task step the
// fleet sweep can no longer reach once the row is pruned); run it in the
// background too.
//
// Like NukeSingleRoleAs it never touches the resident itself or a graduate
// awaiting its restart, and preserves a task bound to another role.
func NukeResidentWorker(d *db.DB, runner NukeRunner, r *heramodel.RoleView) (followUp func(), err error) {
	return NukeResidentWorkerChecked(d, runner, r, ResidentDeleteOptions{})
}

// ErrWorkerSessionStillRunning is returned by NukeResidentWorkerChecked when
// the worker's session was told to stop but had not exited (or its presence
// could not be read) within the stop wait. Nothing is deleted then: a process
// still running could keep writing after the recheck.
var ErrWorkerSessionStillRunning = errors.New("worker session did not exit after stop")

// ErrWorkerStopFailed is returned when the Stop call itself failed. Nothing is
// deleted. In supervisor mode a failed (timed-out) Stop RPC was usually still
// delivered, so the delete waits out the exit wait before deciding whether
// the stop took (see stopResidentWorkerSession).
var ErrWorkerStopFailed = errors.New("stop worker session failed")

// ErrHostBusy is returned, with nothing changed, when the delete reaches the
// point of its first change (the stop, or the nuke for a force delete) too
// late for the rest to finish inside the caller's deadline.
var ErrHostBusy = errors.New("host busy; retry")

// Clock is the time source for the resident delete's waits. Nil fields use
// the real clock; tests substitute a fake one.
type Clock struct {
	NowFn   func() time.Time
	SleepFn func(time.Duration)
}

// Now is the clock's current time.
func (c Clock) Now() time.Time {
	if c.NowFn != nil {
		return c.NowFn()
	}
	return time.Now()
}

// Sleep waits d on the clock.
func (c Clock) Sleep(d time.Duration) {
	if c.SleepFn != nil {
		c.SleepFn(d)
		return
	}
	time.Sleep(d)
}

// StopWait bounds how long the checked delete waits for a stopped worker
// session to actually exit before it runs the recheck. Stop only signals
// (SIGTERM now, SIGKILL to the process group 5s after the stop runs on the
// session owner's side), so the session and its process-group children can
// keep writing after Stop returns. Settle is the minimum time after Stop
// RETURNS before the recheck may run even once the session is gone: a
// background job that traps SIGTERM and does not hold the PTY outlives the
// session until the group SIGKILL, and the supervisor starts that timer only
// when its own stop runs, which a slow RPC delays. Max is measured from the
// start of the Stop call, so it covers the RPC too.
type StopWait struct {
	Max    time.Duration // give up (refuse) when the session is still present this long after the Stop call began
	Settle time.Duration // never recheck sooner than this after Stop returned
	Poll   time.Duration // presence polling interval
}

// DefaultResidentStopWait is the production stop wait. Settle exceeds agent's
// stopKillAfter (5s) so the process-group SIGKILL has fired; Max covers a
// Stop RPC up to the supervisor client's 2s timeout plus Settle. The whole
// call fits the resident delete's overall deadline (residentDeleteDeadline
// in internal/mcp).
var DefaultResidentStopWait = StopWait{Max: 10 * time.Second, Settle: 6 * time.Second, Poll: 100 * time.Millisecond}

func (w StopWait) withDefaults() StopWait {
	if w == (StopWait{}) {
		return DefaultResidentStopWait
	}
	if w.Poll <= 0 {
		w.Poll = DefaultResidentStopWait.Poll
	}
	if w.Settle > w.Max {
		w.Settle = w.Max
	}
	return w
}

// DefaultRemoteBranchDeleteTimeout bounds the asynchronous origin branch
// delete in followUp.
const DefaultRemoteBranchDeleteTimeout = 30 * time.Second

// ResidentDeleteOptions configures NukeResidentWorkerChecked. The zero value
// is NukeResidentWorker: no recheck, no stop wait, no deadline.
type ResidentDeleteOptions struct {
	// Recheck, when set (non-force delete), runs after the worker's session
	// has been stopped and has exited; an error refuses before the nuke.
	Recheck func(*model.Task) error
	// Wait bounds the stop's exit wait (zero = DefaultResidentStopWait).
	Wait StopWait
	// MutateBy, when set, is the latest instant the delete may make its first
	// change (arm the stop guard, or nuke the role when nothing is stopped
	// first). Past it the delete returns ErrHostBusy with nothing changed.
	MutateBy time.Time
	// GiveUpBy, when set, also caps the exit wait (an absolute instant), so
	// the recheck and the local nuke still fit the caller's deadline.
	GiveUpBy time.Time
	// Clock drives every wait; zero = real clock.
	Clock Clock
	// RemoteBranchDelete deletes the worker's branch on origin from followUp
	// (nil = agent.DeleteRemoteBranchContext), leased on expectTip and
	// bounded by RemoteBranchTimeout (zero = DefaultRemoteBranchDeleteTimeout).
	RemoteBranchDelete  func(ctx context.Context, repoDir, branch, expectTip string) error
	RemoteBranchTimeout time.Duration
}

// NukeResidentWorkerChecked is NukeResidentWorker for the non-force delete:
// when the task would be reclaimed (its worktree removed) and opts.Recheck is
// set, it first STOPS the worker's session, waits for it to exit (see
// StopWait), and then runs the recheck, and only nukes the role once the
// recheck passes. A session that kept running while the caller classified the
// work could otherwise write files the worktree removal destroys unseen. A
// stop failure (ErrWorkerStopFailed), an exit-wait timeout
// (ErrWorkerSessionStillRunning) or a recheck refusal returns before anything
// is nuked, keeping the role, binding, task and worktree; the stopped session
// is the only side effect, and a later resume recovers it — the stop never
// leaves the worker looking finished (see stopCloseoutGuard). The recheck is
// skipped when the task is preserved for another live binding, since nothing
// is removed then. ErrHostBusy (opts.MutateBy passed, or the repository's
// cleanup lock, which a reclaim takes before its first change, still held
// at MutateBy) changes nothing, and so does an unreadable binding list.
func NukeResidentWorkerChecked(d *db.DB, runner NukeRunner, r *heramodel.RoleView, opts ResidentDeleteOptions) (followUp func(), err error) {
	noop := func() {}
	if r == nil || r.TaskID == "" {
		return noop, errors.New("no task to delete")
	}
	t, err := d.Get(r.TaskID)
	if err != nil {
		return noop, fmt.Errorf("load task: %w", err)
	}
	if t.IsResident() {
		return noop, errors.New("resident delete must never target itself")
	}
	// CheckNukeTask surfaces the real error for an unreadable marker instead of
	// reporting it as a pending graduation.
	if err := CheckNukeTask(d, t.ID); err != nil {
		residentDeleteLog("[resident] delete role %d skipped task=%s: %v", r.RoleID, t.ID, err)
		return noop, err
	}
	// Fail closed: an unreadable binding list must not read as "bound
	// elsewhere", which would skip the stop and the recheck while the nuke
	// still ends the only binding user_deleted, handing a possibly live
	// worktree to the async reclaim sweep.
	reclaim := false
	if r.Live {
		live, lErr := d.ListHeraLiveBindingsByTask(t.ID)
		if lErr != nil {
			residentDeleteLog("[resident] delete role %d refused task=%s: live bindings unreadable: %v; nothing changed", r.RoleID, t.ID, lErr)
			return noop, fmt.Errorf("read live bindings of task %s: %w; nothing changed", t.ID, lErr)
		}
		reclaim = len(live) == 1 && live[0].RoleID == r.RoleID
	}
	if !opts.MutateBy.IsZero() {
		if now := opts.Clock.Now(); now.After(opts.MutateBy) {
			residentDeleteLog("[resident] delete role %d refused task=%s: host busy (first change due by %s, now %s late); nothing changed", r.RoleID, t.ID, opts.MutateBy.Format(time.RFC3339Nano), now.Sub(opts.MutateBy).Round(time.Millisecond))
			return noop, fmt.Errorf("%w: too little of the call's time budget left to stop and delete safely; nothing changed", ErrHostBusy)
		}
	}
	// The worktree removal needs the per-repo cleanup lock, which other
	// daemon-side cleanups (the reclaim sweep, REST/bulk delete, prune; the
	// lock is in-process, so TUI-process nukes never contend) hold across an untimed origin
	// push. Take it now, before the first change, with a wait bounded by
	// MutateBy, and hold it through the stop, exit wait, recheck and local
	// removal, so nothing after the stop can block on it.
	if reclaim && t.Worktree != "" {
		unlock, lErr := lockResidentRepo(agent.ResolveDir(t, d.Config()), opts, r.RoleID, t.ID)
		if lErr != nil {
			return noop, lErr
		}
		// Released when this returns, before the caller runs followUp.
		defer unlock()
		return nukeResidentWorkerLocked(d, runner, r, t, reclaim, opts, true)
	}
	return nukeResidentWorkerLocked(d, runner, r, t, reclaim, opts, false)
}

// residentRepoLockPoll is how often the bounded repo-lock wait retries.
const residentRepoLockPoll = 20 * time.Millisecond

// lockResidentRepo takes repoDir's cleanup lock for the resident delete. With
// a MutateBy the wait is bounded by it on opts.Clock and runs out as
// ErrHostBusy with nothing changed; without one it waits.
func lockResidentRepo(repoDir string, opts ResidentDeleteOptions, roleID int64, taskID string) (func(), error) {
	if opts.MutateBy.IsZero() {
		return agent.LockRepoContext(context.Background(), repoDir)
	}
	start := opts.Clock.Now()
	for {
		now := opts.Clock.Now()
		if now.After(opts.MutateBy) {
			residentDeleteLog("[resident] delete role %d refused task=%s: host busy (repo cleanup lock %q held by another cleanup for %s); nothing changed", roleID, taskID, repoDir, now.Sub(start).Round(time.Millisecond))
			return nil, fmt.Errorf("%w: another worktree cleanup in this repository held its lock past the call's time budget; nothing changed", ErrHostBusy)
		}
		if unlock, ok := agent.TryLockRepo(repoDir); ok {
			return unlock, nil
		}
		opts.Clock.Sleep(residentRepoLockPoll)
	}
}

// nukeResidentWorkerLocked is NukeResidentWorkerChecked after its pre-change
// checks; repoLocked reports whether the caller holds the task repo's cleanup
// lock (always so when a worktree is reclaimed).
func nukeResidentWorkerLocked(d *db.DB, runner NukeRunner, r *heramodel.RoleView, t *model.Task, reclaim bool, opts ResidentDeleteOptions, repoLocked bool) (followUp func(), err error) {
	noop := func() {}
	if reclaim && opts.Recheck != nil {
		guard := armStopCloseoutGuard(d, t.ID)
		stopped, err := stopResidentWorkerSession(runner, t, opts.Wait.withDefaults(), opts.Clock, opts.GiveUpBy)
		if err != nil {
			guard.refused(stopped)
			residentDeleteLog("[resident] delete role %d refused task=%s: %v", r.RoleID, t.ID, err)
			return noop, err
		}
		if err := opts.Recheck(t); err != nil {
			guard.refused(true)
			residentDeleteLog("[resident] delete role %d refused after session stop task=%s: %v (role, binding and worktree kept)", r.RoleID, t.ID, err)
			return noop, err
		}
	}
	if err := nukeRoleRow(d, r.RoleID, db.HeraActorResident); err != nil {
		residentDeleteLog("[resident] delete role %d: nuke failed task=%s: %v", r.RoleID, t.ID, err)
		return noop, fmt.Errorf("nuke role %d: %w", r.RoleID, err)
	}
	if !reclaim {
		residentDeleteLog("[resident] delete role %d: task=%s preserved (bound elsewhere)", r.RoleID, t.ID)
		return noop, nil
	}
	archiveNukedTask(d, t.ID)
	remoteDir, remoteBranch, leaseTip := reclaimResidentWorkerTask(d, runner, t, repoLocked)
	return func() {
		residentRemoteBranchDelete(opts, t.ID, remoteDir, remoteBranch, leaseTip)
		residentWorkerStackFollowUp(d, t)
	}, nil
}

// residentRemoteBranchDelete is followUp's origin branch delete: its own
// timeout, leased on the tip captured at local removal, outcome logged either
// way. A lease that no longer matches (a same-named branch pushed since, or
// already gone) leaves the branch in place; that is not retried.
func residentRemoteBranchDelete(opts ResidentDeleteOptions, taskID, repoDir, branch, leaseTip string) {
	if branch == "" || repoDir == "" {
		return
	}
	if leaseTip == "" {
		residentDeleteLog("[resident] delete: remote branch left in place task=%s branch=%s (its tip was unknown at local removal; an unleased delete could remove a re-created branch)", taskID, branch)
		return
	}
	del := opts.RemoteBranchDelete
	if del == nil {
		del = agent.DeleteRemoteBranchContext
	}
	timeout := opts.RemoteBranchTimeout
	if timeout <= 0 {
		timeout = DefaultRemoteBranchDeleteTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := del(ctx, repoDir, branch, leaseTip)
	switch {
	case errors.Is(err, agent.ErrRemoteBranchGone):
		residentDeleteLog("[resident] delete: remote branch already gone task=%s branch=%s", taskID, branch)
	case errors.Is(err, agent.ErrRemoteBranchChanged):
		residentDeleteLog("[resident] delete: remote branch changed on origin; left in place task=%s branch=%s: %v", taskID, branch, err)
	case err != nil:
		residentDeleteLog("[resident] delete: remote branch delete failed task=%s branch=%s: %v", taskID, branch, err)
	default:
		residentDeleteLog("[resident] delete: remote branch deleted task=%s branch=%s", taskID, branch)
	}
}

// stopCloseoutGuard keeps the delete's own stop from closing the worker out.
// Stopping an in_progress worker fires the session-exit hook
// (transitionTaskOnExit → RollHeraWorkerToReview), which moves it to
// in_review AND stamps hera.ready_to_close — so a worker the delete then
// refuses would look finished (rail ready-to-close glyph, revive refused,
// closed-out banner). Every exit-hook site acts only on an in_progress task,
// so the guard first flips the task to in_review itself WITHOUT the stamp
// (RollHeraWorkerFailed: the same worker-only, in_progress-only flip, minus
// ready_to_close); the hook is then a no-op even when it runs late, as the
// supervisor's exit relay can. On a refusal it also clears a ready_to_close
// stamp that appeared during the delete (belt and braces), but never one the
// worker already carried before the stop.
type stopCloseoutGuard struct {
	d        *db.DB
	taskID   string
	hadStamp bool
	flipped  bool
}

func armStopCloseoutGuard(d *db.DB, taskID string) *stopCloseoutGuard {
	g := &stopCloseoutGuard{d: d, taskID: taskID}
	had, err := readyToCloseStamped(d, taskID)
	if err != nil {
		// Unknown prior state: never clear a stamp we cannot prove is ours.
		had = true
		residentDeleteLog("[resident] delete: ready_to_close read failed task=%s: %v (stamp left alone on refusal)", taskID, err)
	}
	g.hadStamp = had
	flipped, err := d.RollHeraWorkerFailed(taskID)
	if err != nil {
		residentDeleteLog("[resident] delete: pre-stop in_review flip failed task=%s: %v", taskID, err)
	}
	g.flipped = flipped
	return g
}

// refused undoes the guard's close-out side effects after a refusal.
// stopped reports whether the session must be treated as stopped (gone,
// going, or unknown). It is false only when the Stop failed and a successful
// probe after the exit wait showed the session still running; then the
// pre-stop flip is reverted so a still-running worker is not left parked
// in_review. An unknown outcome keeps the flip: in_review without the stamp
// is resumable, while a reverted flip would let a late exit relay stamp
// ready_to_close and make the worker look finished.
func (g *stopCloseoutGuard) refused(stopped bool) {
	if g.flipped && !stopped {
		if _, err := g.d.ReviveHeraWorkerToInProgress(g.taskID); err != nil {
			residentDeleteLog("[resident] delete: revert pre-stop flip failed task=%s: %v", g.taskID, err)
		}
	}
	if g.hadStamp {
		return
	}
	now, err := readyToCloseStamped(g.d, g.taskID)
	if err != nil || !now {
		return
	}
	if err := g.d.ClearHeraReadyToClose(g.taskID); err != nil {
		residentDeleteLog("[resident] delete: clear ready_to_close after refusal failed task=%s: %v", g.taskID, err)
		return
	}
	residentDeleteLog("[resident] delete: cleared ready_to_close stamped by the delete's own stop task=%s", g.taskID)
}

func readyToCloseStamped(d *db.DB, taskID string) (bool, error) {
	meta, err := d.ListMeta(taskID, db.HeraMetaNamespace)
	if err != nil {
		return false, err
	}
	for _, e := range meta {
		if e.Key == db.HeraMetaKeyReadyToClose && e.Value == "true" {
			return true, nil
		}
	}
	return false, nil
}

// stopResidentWorkerSession stops t's session ahead of the recheck and waits
// for it to exit. Presence comes from SessionPresent, and an unreadable
// presence (a supervisor RPC error or timeout) always counts as PRESENT: it
// never skips the stop and never ends the wait early. A session already
// absent needs no stop. The wait refuses once the Stop call began wait.Max
// ago (or at giveUpBy, whichever is first) with the session still present or
// unreadable, and only succeeds once the session is gone AND wait.Settle has
// passed since Stop returned.
//
// A failed Stop is not trusted either way: a timed-out supervisor RPC was
// usually delivered. The delete is refused (ErrWorkerStopFailed), but only
// after the exit wait decides what happened. stopped reports whether the
// caller must treat the session as stopped (keep the pre-stop in_review flip):
// false only when the Stop failed AND a successful probe at the end shows the
// session still running, i.e. the stop did not take.
func stopResidentWorkerSession(runner NukeRunner, t *model.Task, wait StopWait, clk Clock, giveUpBy time.Time) (stopped bool, err error) {
	if runner == nil {
		return true, nil
	}
	present, perr := SessionPresent(runner, t.ID)
	if perr == nil && !present {
		return true, nil
	}
	if perr != nil {
		residentDeleteLog("[resident] delete: session presence unreadable task=%s: %v (counted as present; stopping)", t.ID, perr)
	}
	start := clk.Now()
	limit := start.Add(wait.Max)
	if !giveUpBy.IsZero() && giveUpBy.Before(limit) {
		limit = giveUpBy
	}
	stopErr := runner.Stop(t.ID)
	if errors.Is(stopErr, agent.ErrSessionNotFound) {
		stopErr = nil
	}
	returned := clk.Now()
	if stopErr != nil {
		residentDeleteLog("[resident] delete: stop failed task=%s after %s: %v (waiting to see whether it took)", t.ID, returned.Sub(start).Round(time.Millisecond), stopErr)
	}
	for {
		present, perr := SessionPresent(runner, t.ID)
		gone := perr == nil && !present
		now := clk.Now()
		if gone && stopErr != nil {
			return true, fmt.Errorf("%w: %w (the session has since exited); nothing deleted", ErrWorkerStopFailed, stopErr)
		}
		if gone && now.Sub(returned) >= wait.Settle {
			residentDeleteLog("[resident] delete: session exited after stop task=%s waited=%s since stop returned", t.ID, now.Sub(returned).Round(time.Millisecond))
			return true, nil
		}
		if !now.Before(limit) {
			waited := now.Sub(start).Round(time.Millisecond)
			switch {
			case stopErr != nil && perr == nil:
				// A successful probe shows it still running: the stop did not take.
				return false, fmt.Errorf("%w: %w (the session is still running); nothing deleted", ErrWorkerStopFailed, stopErr)
			case stopErr != nil:
				return true, fmt.Errorf("%w: %w (session state unreadable: %w); nothing deleted", ErrWorkerStopFailed, stopErr, perr)
			case gone:
				return true, fmt.Errorf("%w: it exited but the stop had not settled within %s; nothing deleted", ErrWorkerSessionStillRunning, waited)
			case perr != nil:
				return true, fmt.Errorf("%w within %s (session state unreadable: %w); nothing deleted", ErrWorkerSessionStillRunning, waited, perr)
			default:
				return true, fmt.Errorf("%w within %s; nothing deleted", ErrWorkerSessionStillRunning, waited)
			}
		}
		clk.Sleep(wait.Poll)
	}
}

// reclaimResidentWorkerTask is ReconcileHeraReclaims' per-candidate body for
// exactly one task, minus the stacked-branch re-discovery and the origin
// branch delete (both in followUp), so it is local and bounded. It returns
// the repo dir and branch to delete on origin. An unreadable session presence
// counts as present: the session is stopped anyway and the prune is left to
// the reclaim sweep.
func reclaimResidentWorkerTask(d *db.DB, runner NukeRunner, t *model.Task, repoLocked bool) (remoteDir, remoteBranch, leaseTip string) {
	if runner != nil {
		present, perr := SessionPresent(runner, t.ID)
		if perr != nil {
			residentDeleteLog("[resident] delete: session presence unreadable task=%s: %v (stopping anyway)", t.ID, perr)
		}
		if perr != nil || present {
			if err := runner.Stop(t.ID); err != nil && !errors.Is(err, agent.ErrSessionNotFound) {
				residentDeleteLog("[resident] delete: stop session failed task=%s: %v", t.ID, err)
			}
		}
	}
	if t.Worktree != "" && agent.DirExists(t.Worktree) {
		remove := agent.RemoveWorktreeAndLocalBranch
		if repoLocked {
			remove = agent.RemoveWorktreeAndLocalBranchLocked
		}
		remoteDir, remoteBranch, leaseTip = remove(t.Worktree, t.Branch, agent.ResolveDir(t, d.Config()))
		residentDeleteLog("[resident] delete: removed worktree+local branch task=%s (origin branch delete runs in the background)", t.ID)
	}
	if runner != nil {
		present, perr := SessionPresent(runner, t.ID)
		if perr != nil || present {
			residentDeleteLog("[resident] delete: prune deferred to reclaim sweep task=%s (session live or unreadable: %v)", t.ID, perr)
			return remoteDir, remoteBranch, leaseTip
		}
	}
	pruned, _, err := d.PruneTasks([]string{t.ID})
	switch {
	case err != nil:
		residentDeleteLog("[resident] delete: prune failed task=%s: %v", t.ID, err)
	case len(pruned) == 0:
		residentDeleteLog("[resident] delete: prune skipped task=%s (deferred to reclaim sweep)", t.ID)
	default:
		residentDeleteLog("[resident] delete: pruned task row %s", t.ID)
	}
	return remoteDir, remoteBranch, leaseTip
}

func residentWorkerStackFollowUp(d *db.DB, x *model.Task) {
	all, err := d.Tasks()
	if err != nil {
		residentDeleteLog("[resident] delete: stacked-branch follow-up skipped task=%s: %v", x.ID, err)
		return
	}
	byBase := make(map[string]*model.Task, len(all))
	for _, t := range all {
		if t.BaseBranch != "" {
			byBase[t.BaseBranch] = t
		}
	}
	n := reconcileStackedBranches(context.Background(), d, d.Config(), byBase, x, map[string]mergesafety.Verdict{})
	residentDeleteLog("[resident] delete: stacked-branch follow-up task=%s deleted=%d", x.ID, n)
}
