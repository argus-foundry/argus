package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// Shared with graduation: target membership must stay live from validation
// through the destructive mutation. Never hold this during a restart wait.
var residentWorkerMutationMu sync.Mutex

var residentDeleteTool = Tool{Name: "hera_delete_worker", Description: "Resident only: delete a live worker of your standing team. Refuses a busy worker, unmerged non-empty work, any uncommitted or untracked files, and work on a detached HEAD or in a paused rebase/merge, unless force=true (a refusal found after stopping the session leaves it stopped but resumable). Force returns the unmerged summary; REPORT it to Aaron in your own conversation in this turn.", InputSchema: map[string]interface{}{
	"type": "object", "properties": map[string]interface{}{"role": map[string]interface{}{"type": "string", "description": "Worker name or numeric role id"}, "force": map[string]interface{}{"type": "boolean"}}, "required": []string{"role"},
}}

func (s *Server) boundResident(caller string) bool {
	if caller == "" || s.taskDB == nil {
		return false
	}
	t, err := s.taskDB.Get(caller)
	return err == nil && t != nil && t.IsResident()
}

// residentPromotionGuard runs before the coordinator scan and fails closed on
// unreadable membership or marker state. The marker also covers pre-launch.
func (s *Server) residentPromotionGuard(t *model.Task) error {
	if t == nil {
		return fmt.Errorf("cannot classify resident caller")
	}
	if t.IsResident() {
		return fmt.Errorf("the resident already owns its standing orchestrator; use hera_spawn_worker with an explicit project instead")
	}
	if d, ok := s.heraStore.(interface{ IsResidentWorker(string) (bool, error) }); ok {
		yes, err := d.IsResidentWorker(t.ID)
		if err != nil {
			return fmt.Errorf("resident worker check failed: %w", err)
		}
		if yes {
			return fmt.Errorf("you are a resident worker; ask the resident to graduate you (hera_send), do not create an orchestrator yourself")
		}
	} else {
		return fmt.Errorf("cannot classify resident worker membership")
	}
	return nil
}

func (s *Server) toolHeraNewOrchestratorAs(id interface{}, args json.RawMessage, caller string) *Response {
	if !s.heraEnabled() {
		return residentToolError(id, "hera not configured")
	}
	if caller != "" {
		t, err := s.taskDB.Get(caller)
		if err != nil {
			return residentToolError(id, "cannot verify bound caller")
		}
		if err = s.residentPromotionGuard(t); err != nil {
			return residentToolError(id, err.Error())
		}
		var p map[string]interface{}
		if err = json.Unmarshal(args, &p); err != nil {
			return residentToolError(id, err.Error())
		}
		if p == nil {
			return residentToolError(id, "invalid Hera arguments")
		}
		p["cwd"] = t.Worktree
		args, _ = json.Marshal(p)
	}
	return s.toolHeraNewOrchestrator(id, args)
}

func (s *Server) toolHeraDeleteWorker(id interface{}, args json.RawMessage, caller string) *Response {
	// One overall deadline for the whole call, started before queueing on the
	// mutation lock so the wait for another resident mutation counts.
	deadline := residentClock.Now().Add(residentDeleteDeadline)
	// Identity comes only from the transport's caller binding. Ignore cwd/id.
	if !s.boundResident(caller) {
		return residentToolError(id, "hera_delete_worker is resident only (connection-bound caller required)")
	}
	residentWorkerMutationMu.Lock()
	defer residentWorkerMutationMu.Unlock()
	d, ok := s.taskDB.(*db.DB)
	if !ok || !s.heraEnabled() {
		return residentToolError(id, "resident delete not configured")
	}
	runner, ok := s.taskStopper.(hera.NukeRunner)
	if !ok {
		return residentToolError(id, "resident delete runner not configured")
	}
	var p struct {
		Role  string `json:"role"`
		Force bool   `json:"force"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return residentToolError(id, err.Error())
	}
	orch, err := d.HeraOrchestratorByName(db.ResidentOrchestratorName)
	if err != nil {
		return residentToolError(id, err.Error())
	}
	own, err := d.HeraLiveBindingByTaskAndOrchestrator(caller, orch.ID)
	if err != nil {
		return residentToolError(id, "resident standing binding unavailable")
	}
	role, err := d.HeraRoleByName(orch.ID, p.Role)
	if err != nil {
		if rid, e := strconv.ParseInt(p.Role, 10, 64); e == nil {
			role, err = d.HeraRole(rid)
		}
	}
	if err != nil || role == nil || role.OrchestratorID != orch.ID {
		return residentToolError(id, "target is not a live worker of the resident orchestrator")
	}
	if role.ID == own.RoleID {
		return residentToolError(id, "resident delete must never target itself")
	}
	if role.Kind != db.HeraKindWorker || role.NukedAt != nil {
		return residentToolError(id, "target is not a live worker of the resident orchestrator")
	}
	b, err := d.HeraLiveBindingByRole(role.ID)
	if err != nil {
		return residentToolError(id, "target is not a live worker of the resident orchestrator (possibly graduated)")
	}
	task, err := d.Get(b.ArgusTaskID)
	if err != nil {
		return residentToolError(id, err.Error())
	}
	if task.IsResident() || task.ID == caller {
		return residentToolError(id, "resident delete must never target itself")
	}
	cfg := d.Config()
	pr := ""
	entries, metaErr := d.ListMeta(task.ID, "pr")
	if metaErr == nil {
		for _, e := range entries {
			if e.Key == "state" {
				pr = e.Value
			}
		}
	} else {
		pr = fmt.Sprintf("unknown (%v)", metaErr)
	}
	// Non-force order: idle check, branch-tip capture, summary + clean-tree
	// check, classification, stop, wait for exit, re-check, delete. Everything
	// before the stop runs while the session may still be live, so the
	// re-check after the exit is what guarantees nothing written meanwhile is
	// destroyed unseen. The tip is captured FIRST: a commit landing after it
	// (even between the summary and classification, where a stale empty
	// summary would wave an unsafe verdict through) moves the tip and the
	// re-check refuses.
	//
	// Deadline: everything before the first change (the stop; the nuke for a
	// force delete) must leave the worst-case remainder of the call inside
	// the overall deadline (mutateBy), so idle sampling and classification
	// are capped by it, and past it the call refuses "host busy; retry" with
	// nothing changed. After the stop everything is local and bounded; the
	// origin branch delete runs in the background (see residentDeleteDeadline).
	reserve := residentForceReserve
	if !p.Force {
		reserve = residentPostStopReserve()
	}
	mutateBy := deadline.Add(-reserve)
	if now := residentClock.Now(); !now.Before(mutateBy) {
		return residentHostBusy(id, role.Name, task.ID, fmt.Sprintf("waited %s for another resident worker mutation or session queries", now.Sub(deadline.Add(-residentDeleteDeadline)).Round(time.Millisecond)))
	}
	if !p.Force {
		if err := residentWorkerIdle(runner, task.ID, mutateBy); err != nil {
			if errors.Is(err, hera.ErrHostBusy) {
				return residentHostBusy(id, role.Name, task.ID, "the idle check ran out of the call's time budget")
			}
			residentMCPLog("[resident] delete worker=%s task=%s refused: %v", role.Name, task.ID, err)
			return residentToolError(id, err.Error())
		}
	}
	ctxDeadline := residentClock.Now().Add(residentClassifyBudget)
	if mutateBy.Before(ctxDeadline) {
		ctxDeadline = mutateBy
	}
	ctx, cancel := context.WithTimeout(context.Background(), ctxDeadline.Sub(residentClock.Now()))
	defer cancel()
	tip := ""
	if !p.Force {
		tip = residentBranchTip(ctx, task)
	}
	summary := residentSummarize(ctx, cfg, task, pr)
	var recheck func(*model.Task) error
	if !p.Force {
		// An expired budget makes the summary's fields unknown; that is not
		// dirt, and saying so would mislead.
		if ctx.Err() != nil {
			return residentHostBusy(id, role.Name, task.ID, "the work summary ran out of the call's time budget")
		}
		// A safe verdict speaks only for the branch: uncommitted or untracked
		// files would be destroyed with the worktree. Unknown counts as dirty.
		if summary.DirtyCount != "0" {
			return residentToolError(id, fmt.Sprintf("not safe: worktree has uncommitted or untracked changes (dirty files: %s); commit them, or pass force=true", summary.DirtyCount))
		}
		// Commits on a detached HEAD and a paused rebase/merge are invisible
		// to the clean-tree and branch-tip checks but die with the worktree.
		if err := residentWorktreeStateFn(ctx, task); err != nil {
			if ctx.Err() != nil {
				return residentHostBusy(id, role.Name, task.ID, "the worktree state check ran out of the call's time budget")
			}
			return residentToolError(id, fmt.Sprintf("not safe: %v; finish it on the worker's branch, or pass force=true", err))
		}
		classify := s.residentClassify
		if classify == nil {
			classify = func(ctx context.Context, t *model.Task) (mergesafety.Verdict, error) {
				return mergesafety.ClassifyTask(ctx, cfg, t)
			}
		}
		v, err := classify(ctx, task)
		if err != nil {
			if ctx.Err() != nil {
				return residentHostBusy(id, role.Name, task.ID, fmt.Sprintf("the merge-safety check ran out of the call's time budget (%v)", err))
			}
			return residentToolError(id, fmt.Sprintf("not safe: classifier failed: %v", err))
		}
		if !v.Safe && !summary.Empty() {
			return residentToolError(id, fmt.Sprintf("not safe: %s; commits ahead: %s; dirty files: %s", v.Reason, summary.CommitsAhead, summary.DirtyCount))
		}
		recheck = func(t *model.Task) error {
			// Fresh deadline: classification may have used up ctx, and an
			// expired re-check would refuse for the wrong reason. It is part
			// of the post-stop reserve.
			rctx, rcancel := context.WithTimeout(context.Background(), residentRecheckBudget)
			defer rcancel()
			return residentRecheckTree(rctx, cfg, t, tip)
		}
	}
	if p.Force {
		// The tool result is the summary's only delivery to the resident, and the
		// nuke below can outlast a client-side MCP timeout. Persist it first.
		raw, _ := json.Marshal(summary)
		uxlog.Log("[resident] force delete worker=%s task=%s summary=%s", role.Name, task.ID, raw)
		slog.Warn("[resident] force delete", "worker", role.Name, "task", task.ID, "summary", string(raw))
	}
	// Role-scoped stop and reclaim are synchronous so the result follows them;
	// the fleet-wide sweep is not, so a slow sweep never delays the summary.
	var refusal error
	if recheck != nil {
		inner := recheck
		recheck = func(t *model.Task) error {
			refusal = inner(t)
			return refusal
		}
	}
	followUp, err := hera.NukeResidentWorkerChecked(d, runner, &heramodel.RoleView{RoleID: role.ID, TaskID: task.ID, Live: true}, hera.ResidentDeleteOptions{
		Recheck:  recheck,
		Wait:     residentStopWait,
		MutateBy: mutateBy,
		// The exit wait must leave the re-check and the local nuke their time.
		GiveUpBy:           deadline.Add(-(residentRecheckBudget + residentLocalNukeReserve)),
		Clock:              residentClock,
		RemoteBranchDelete: residentRemoteBranchDelete,
	})
	if errors.Is(err, hera.ErrHostBusy) {
		return residentHostBusy(id, role.Name, task.ID, strings.TrimPrefix(err.Error(), hera.ErrHostBusy.Error()+": "))
	}
	if refusal != nil {
		return residentToolError(id, fmt.Sprintf("not deleted: %v; worker %s's session was stopped and can be resumed (it is not marked finished); its role, binding and worktree are kept; commit the work, or pass force=true", refusal, role.Name))
	}
	if errors.Is(err, hera.ErrWorkerSessionStillRunning) {
		return residentToolError(id, fmt.Sprintf("not deleted: %v; worker %s was told to stop and can be resumed once it exits; its role, binding and worktree are kept; retry, or pass force=true", err, role.Name))
	}
	if errors.Is(err, hera.ErrWorkerStopFailed) {
		return residentToolError(id, fmt.Sprintf("not deleted: %v; worker %s's role, binding and worktree are kept and it can be resumed (it is not marked finished); retry, or pass force=true", err, role.Name))
	}
	if err != nil {
		return residentToolError(id, fmt.Sprintf("worker nuke did not complete: %v", err))
	}
	s.runResidentFleetReconcile(d, runner, role.Name, followUp)
	updated, err := d.HeraRole(role.ID)
	if err != nil || updated.NukedAt == nil {
		return residentToolError(id, "worker nuke did not complete")
	}
	uxlog.Log("[resident] deleted worker=%s task=%s force=%t", role.Name, task.ID, p.Force)
	slog.Info("[resident] deleted worker", "worker", role.Name, "task", task.ID, "force", p.Force)
	var out strings.Builder
	fmt.Fprintf(&out, "Deleted worker %s.\n", role.Name)
	if p.Force {
		raw, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Fprintf(&out, "\nUnmerged summary (REPORT to Aaron now):\n%s\n", raw)
	}
	return toolResult(id, out.String())
}

// residentSessionReader is the runner surface the non-force delete needs to
// tell a busy worker from an idle or absent one. Both the in-process runner
// and the daemon's supervisor client provide it.
type residentSessionReader interface {
	Get(taskID string) agent.SessionHandle
	HasPendingRestart(taskID string) bool
}

// hera_delete_worker time budget. The MCP client deadline is 60s
// (gotchas/daemon-rpc.md). The whole call — force or not, INCLUDING time
// queued on residentWorkerMutationMu — runs under residentDeleteDeadline,
// measured from handler entry. The post-stop section is local and bounded:
// the stop's exit wait (residentStopWait.Max, which covers the Stop RPC),
// the re-check (residentRecheckBudget) and the local nuke
// (residentLocalNukeReserve: DB rows, presence probes, worktree and local
// branch removal); the origin branch delete runs asynchronously after the
// result. Whatever precedes the first change (queueing, idle sampling,
// tip/summary/classification, at most residentClassifyBudget) gets what is
// left, and past that point the call refuses "host busy; retry" with nothing
// changed; that includes waiting for the repository's cleanup lock, which
// the delete takes before its first change and holds through the local
// removal. A force delete reserves only its nuke (residentForceReserve).
const (
	residentDeleteDeadline   = 45 * time.Second
	residentClassifyBudget   = 24 * time.Second
	residentRecheckBudget    = 10 * time.Second
	residentLocalNukeReserve = 8 * time.Second
	residentForceReserve     = residentLocalNukeReserve + 4*time.Second // + the reclaim's Stop RPC and presence probes
)

// residentPostStopReserve is what a non-force delete must still have left
// when it stops the worker's session.
func residentPostStopReserve() time.Duration {
	return residentStopWait.Max + residentRecheckBudget + residentLocalNukeReserve
}

// residentStopWait is the stop exit wait; tests shorten it.
var residentStopWait = hera.DefaultResidentStopWait

// residentClock drives the delete's deadline, idle sampling and exit wait;
// zero = real clock, tests substitute a fake one.
var residentClock hera.Clock

// residentRemoteBranchDelete deletes the deleted worker's branch on origin in
// the background (nil = agent.DeleteRemoteBranchContext); tests block it.
var residentRemoteBranchDelete func(ctx context.Context, repoDir, branch, expectTip string) error

// residentHostBusy refuses a delete that ran out of its time budget before
// changing anything.
func residentHostBusy(id interface{}, worker, taskID, why string) *Response {
	residentMCPLog("[resident] delete worker=%s task=%s refused: host busy (%s); nothing changed", worker, taskID, why)
	return residentToolError(id, fmt.Sprintf("not deleted: host busy; retry (%s); worker %s is unchanged", why, worker))
}

// residentSummarize is SummarizeTask; tests wrap it to land work mid-delete.
var residentSummarize = mergesafety.SummarizeTask

// residentIdleSampling bounds the content-aware idle check: a session whose
// raw PTY output never goes quiet (an animated parked prompt, BUG-083) is
// sampled every Interval for up to Window (on residentClock), and never past
// the delete's time budget. Interval*ticks within Window must reach both
// agent's 3s content-stability threshold and the NeedsInputEscalationTicks
// (8) parked-selection escalation.
type residentIdleSamplingCfg struct {
	Window   time.Duration
	Interval time.Duration
}

const residentIdleWindow = 6 * time.Second

var residentIdleSampling = residentIdleSamplingCfg{Window: residentIdleWindow, Interval: 500 * time.Millisecond}

// residentWorkerIdle refuses unless taskID's session is idle or absent. It
// fails closed when the runner cannot report session state (including an
// unreadable presence: a supervisor RPC error is never "absent"), and treats
// a queued restart (a session about to come back) as busy. A session that is
// not raw-idle gets a bounded content-aware second look (residentContentIdle)
// so a parked prompt that keeps animating is not refused forever; that look
// never runs past until, and one the budget cut short returns
// hera.ErrHostBusy.
func residentWorkerIdle(runner hera.NukeRunner, taskID string, until time.Time) error {
	const busy = "worker is busy; wait, or force"
	sr, ok := runner.(residentSessionReader)
	if !ok {
		return fmt.Errorf("%s (cannot read the worker's session state)", busy)
	}
	if sr.HasPendingRestart(taskID) {
		return fmt.Errorf("%s (a session restart is pending)", busy)
	}
	sess := sr.Get(taskID)
	if sess == nil {
		present, err := hera.SessionPresent(runner, taskID)
		if err != nil {
			return fmt.Errorf("%s (cannot read the worker's session state: %w)", busy, err)
		}
		if present {
			return fmt.Errorf("%s (cannot read the worker's session state)", busy)
		}
		return nil
	}
	if !sess.Alive() || sess.IsIdle() {
		return nil
	}
	idle, err := residentContentIdle(taskID, sess, until)
	if err != nil {
		return err
	}
	if !idle {
		return errors.New(busy)
	}
	return nil
}

// residentContentIdle samples sess with a fresh agent.ContentIdleTracker over
// residentIdleSampling.Window and reports whether it became raw-idle,
// content-idle (stable emulated screen, no working affordance, or an escalated
// parked selection prompt), or exited. A session whose content keeps changing
// never converges and stays busy. The window is capped at until; when that
// cap (not the window) ends an unconverged sample, the result is
// hera.ErrHostBusy rather than a busy verdict, since the worker was not given
// a full look.
func residentContentIdle(taskID string, sess agent.SessionHandle, until time.Time) (bool, error) {
	cfg := residentIdleSampling
	clk := residentClock
	var tracker agent.ContentIdleTracker
	start := clk.Now()
	window, capped := cfg.Window, false
	if left := until.Sub(start); left < window {
		window, capped = left, true
	}
	for {
		now := clk.Now()
		if tracker.IsIdle(taskID, sess, now) {
			residentMCPLog("[resident] delete: worker content-idle task=%s after %s", taskID, now.Sub(start).Round(time.Millisecond))
			return true, nil
		}
		if !sess.Alive() {
			return true, nil
		}
		if now.Sub(start) >= window {
			if capped {
				residentMCPLog("[resident] delete: content sampling cut to %s by the time budget task=%s", window.Round(time.Millisecond), taskID)
				return false, fmt.Errorf("%w: idle sampling cut short by the time budget", hera.ErrHostBusy)
			}
			residentMCPLog("[resident] delete: worker still busy task=%s after %s of content sampling", taskID, cfg.Window)
			return false, nil
		}
		clk.Sleep(cfg.Interval)
	}
}

// residentBranchTip records the worker branch's commit before classification;
// an unreadable tip is recorded as "" and must stay unreadable to compare equal.
func residentBranchTip(ctx context.Context, t *model.Task) string {
	if t.Worktree == "" || t.Branch == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--end-of-options", t.Branch+"^{commit}")
	cmd.Dir = t.Worktree
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// residentRecheckTree runs after the worker's session is stopped: the tree
// must still be clean (unknown counts as dirty) and the branch must not have
// moved since classification, or the verdict no longer covers the work.
func residentRecheckTree(ctx context.Context, cfg config.Config, t *model.Task, tipBefore string) error {
	sum := mergesafety.SummarizeTask(ctx, cfg, t, "")
	if sum.DirtyCount != "0" {
		msg := fmt.Sprintf("worktree became dirty while the delete was classifying (dirty files: %s)", sum.DirtyCount)
		if names := residentDirtyFiles(ctx, t.Worktree); len(names) > 0 {
			msg += ": " + strings.Join(names, ", ")
		}
		return errors.New(msg)
	}
	if err := residentWorktreeStateFn(ctx, t); err != nil {
		return err
	}
	if tip := residentBranchTip(ctx, t); tip != tipBefore {
		return fmt.Errorf("branch %s moved while the delete was classifying", t.Branch)
	}
	return nil
}

// residentWorktreeOps are the in-progress operations whose state lives in the
// worktree's git dir; work paused in one is not on the branch yet.
var residentWorktreeOps = []struct{ path, name string }{
	{"rebase-merge", "rebase"},
	{"rebase-apply", "rebase (or am)"},
	{"MERGE_HEAD", "merge"},
	{"CHERRY_PICK_HEAD", "cherry-pick"},
	{"REVERT_HEAD", "revert"},
}

// residentWorktreeStateFn is a test seam for residentWorktreeState.
var residentWorktreeStateFn = residentWorktreeState

// residentWorktreeState refuses (as dirty) a worktree whose work the
// clean-tree and branch-tip checks cannot see: a rebase, merge, cherry-pick
// or revert in progress, or a HEAD that is not refs/heads/<task branch>
// (detached, or another branch), where commits never reach the branch and
// become unreachable when the worktree is removed. Unreadable state fails
// closed.
func residentWorktreeState(ctx context.Context, t *model.Task) error {
	if t.Worktree == "" {
		return nil
	}
	args := []string{"rev-parse"}
	for _, op := range residentWorktreeOps {
		args = append(args, "--git-path", op.path)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = t.Worktree
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("cannot read the worktree's git state: %w", err)
	}
	paths := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(paths) != len(residentWorktreeOps) {
		return fmt.Errorf("cannot read the worktree's git state: unexpected rev-parse output %q", out)
	}
	for i, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(t.Worktree, p)
		}
		_, sErr := os.Lstat(p)
		if sErr == nil {
			return fmt.Errorf("a %s is in progress in the worktree", residentWorktreeOps[i].name)
		}
		if !os.IsNotExist(sErr) {
			return fmt.Errorf("cannot read the worktree's git state: %w", sErr)
		}
	}
	cmd = exec.CommandContext(ctx, "git", "symbolic-ref", "--quiet", "HEAD")
	cmd.Dir = t.Worktree
	out, err = cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && ctx.Err() == nil {
		return fmt.Errorf("the worktree's HEAD is detached (not on branch %s); commits made there would be lost", t.Branch)
	}
	if err != nil {
		return fmt.Errorf("cannot read the worktree's HEAD: %w", err)
	}
	if head := strings.TrimSpace(string(out)); head != "refs/heads/"+t.Branch {
		// An unborn branch (no commit yet) holds nothing the removal could
		// lose; anything else is work off the worker's branch.
		cmd = exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", "HEAD")
		cmd.Dir = t.Worktree
		err = cmd.Run()
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && ctx.Err() == nil {
			return nil
		}
		return fmt.Errorf("the worktree's HEAD is on %s, not the worker's branch %s", strings.TrimPrefix(head, "refs/heads/"), t.Branch)
	}
	return nil
}

// residentDirtyFiles names up to residentDirtyNameLimit dirty paths, best
// effort (nil on any error). Names are %q-quoted so a crafted filename cannot
// inject text into the resident's tool result.
func residentDirtyFiles(ctx context.Context, wt string) []string {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	cmd.Dir = wt
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var names []string
	entries := strings.Split(string(out), "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		if len(names) == residentDirtyNameLimit {
			names = append(names, "...")
			break
		}
		names = append(names, strconv.Quote(e[3:]))
		if e[0] == 'R' || e[0] == 'C' || e[1] == 'R' || e[1] == 'C' {
			i++ // skip the rename/copy source path
		}
	}
	return names
}

const residentDirtyNameLimit = 10

// residentReconcileMu serializes the resident-triggered async reclaim sweeps
// so a burst of deletes cannot run overlapping sweeps over the same
// candidates. It is never held with residentWorkerMutationMu.
var residentReconcileMu sync.Mutex

// runResidentFleetReconcile runs followUp and then the fleet-wide reclaim
// sweep in the background, recovering panics and logging the outcome. Safe
// against a concurrent graduate or delete: the sweep's candidates are tasks
// whose bindings ended user_deleted and hold no live binding (a graduate
// always holds one, and its pending marker is skipped), the deleted task's
// own row was already pruned in the role-scoped section, and PruneTasks
// re-checks for a live binding at delete time.
func (s *Server) runResidentFleetReconcile(d *db.DB, runner hera.SessionChecker, worker string, followUp func()) {
	reconcile := s.residentFleetReconcile
	if reconcile == nil {
		reconcile = hera.ReconcileHeraReclaims
	}
	s.residentBackground.Add(1)
	go func() {
		defer s.residentBackground.Done()
		defer func() {
			if r := recover(); r != nil {
				uxlog.Log("[resident] PANIC in async reclaim sweep after delete worker=%s: %v\n%s", worker, r, debug.Stack())
				slog.Error("[resident] async reclaim sweep panicked", "worker", worker, "panic", fmt.Sprint(r))
			}
		}()
		residentReconcileMu.Lock()
		defer residentReconcileMu.Unlock()
		if followUp != nil {
			followUp()
		}
		sum, err := reconcile(d, runner)
		if err != nil {
			uxlog.Log("[resident] async reclaim sweep after delete worker=%s failed: %v", worker, err)
			slog.Warn("[resident] async reclaim sweep failed", "worker", worker, "err", err)
			return
		}
		uxlog.Log("[resident] async reclaim sweep after delete worker=%s: candidates=%d pruned=%d skipped_live=%d", worker, sum.Candidates, sum.Pruned, sum.SkippedLiveSession)
		slog.Info("[resident] async reclaim sweep done", "worker", worker, "candidates", sum.Candidates, "pruned", sum.Pruned, "skipped_live_session", sum.SkippedLiveSession)
	}()
}

// residentMCPLog writes a [resident] line to both the uxlog and slog. The
// daemon never calls uxlog.Init, so uxlog.Log alone is a silent no-op there
// (same dual-write as internal/daemon's residentLog).
func residentMCPLog(format string, args ...any) {
	uxlog.Log(format, args...)
	slog.Info(fmt.Sprintf(format, args...)) //nolint:gosec // G706: internal diagnostics, not a user-controlled log-forging surface
}

func residentToolError(id interface{}, message string) *Response {
	uxlog.Log("[resident] tool refused: %s", message)
	slog.Warn("[resident] tool refused", "reason", message)
	return toolError(id, message)
}

// verifiedCaller returns the connection-bound caller only when it is the
// resident presenting the current launch's token; every other connection,
// including the resident's id with a missing, wrong or superseded token, is
// unbound (""). The token is never logged.
func (s *Server) verifiedCaller(r *http.Request) string {
	caller := r.URL.Query().Get("caller")
	if caller == "" {
		return ""
	}
	token := r.Header.Get(agent.ResidentMCPTokenHeader)
	hashPath := s.residentTokenHashPath
	if hashPath == nil {
		hashPath = agent.ResidentMCPTokenHashPath
	}
	path, err := hashPath()
	if err == nil && s.boundResident(caller) && agent.VerifyResidentMCPToken(path, token) {
		return caller
	}
	uxlog.Log("[resident] unverified caller refused: caller=%q token_present=%t", caller, token != "")
	slog.Warn("[resident] unverified caller refused", "caller", caller, "token_present", token != "") //nolint:gosec // G706: slog encodes attributes, escaping control characters; the token itself is never logged
	return ""
}
