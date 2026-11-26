package daemon

import (
	"log/slog"

	"github.com/argus-foundry/argus/internal/agent"
)

type reclaimExitOwner interface {
	sessionProber
	agent.HeraReclaimSessionOwner
	ObservedSessionExit(taskID string, pid int) ExitInfo
}

type localReclaimOwner struct {
	agent.HeraReclaimSessionOwner
	core *sessionCore
}

func (o localReclaimOwner) ProbeSession(taskID string) (SessionInfo, error) {
	var info SessionInfo
	err := o.core.SessionStatus(&TaskIDReq{TaskID: taskID}, &info)
	return info, err
}

func (o localReclaimOwner) ObservedSessionExit(taskID string, pid int) ExitInfo {
	var exit ExitInfo
	if err := o.core.GetExitInfo(&TaskIDReq{TaskID: taskID, ExpectedPID: pid}, &exit); err != nil {
		return ExitInfo{StreamLost: true}
	}
	return exit
}

// recoverHeraReclaimExits settles exact supervisor exits that the daemon's
// stream relay missed. The same task lock protects normal exit finalization.
func (d *Daemon) recoverHeraReclaimExits() int {
	owner, ok := d.supClient.(reclaimExitOwner)
	if !ok {
		local, ok := d.runner.(*agent.Runner)
		if !ok {
			return 0
		}
		owner = localReclaimOwner{HeraReclaimSessionOwner: local, core: d.sessionCore}
	}
	ids, err := d.db.PendingHeraReclaimStops()
	if err != nil {
		slog.Warn("hera reclaim: read pending exits failed", "err", err)
		return 0
	}
	recovered := 0
	for _, id := range ids {
		if d.exitCtx.Err() != nil {
			break
		}
		if d.recoverHeraReclaimExit(owner, id) {
			recovered++
		}
	}
	return recovered
}

func (d *Daemon) recoverHeraReclaimExit(owner reclaimExitOwner, taskID string) bool {
	unlock, acquired, err := d.db.TryLockSessionMutation(taskID)
	if err != nil || !acquired {
		slog.Info("hera reclaim: exit recovery lock unavailable", "task", taskID, "err", err)
		return false
	}
	defer unlock()

	pid, err := d.db.HeraReclaimStopPID(taskID)
	if err != nil || pid <= 0 {
		return false
	}
	if err := owner.CheckReclaimSessionRetired(taskID); err != nil {
		d.retryHeraReclaimStop(owner, taskID, pid)
		return false
	}
	info, err := owner.ProbeSession(taskID)
	if err != nil || info.Alive || (info.PID != 0 && info.PID != pid) ||
		(info.LastExitPID != 0 && info.LastExitPID != pid) {
		slog.Info("hera reclaim: owner did not confirm retired process", "task", taskID, "pid", pid, "owner_pid", info.PID, "last_exit_pid", info.LastExitPID, "err", err)
		return false
	}
	exit := owner.ObservedSessionExit(taskID, pid)
	if exit.PID != pid || exit.StreamLost || exit.PendingRestart {
		slog.Info("hera reclaim: exit recovery awaits scoped receipt", "task", taskID, "pid", pid)
		return false
	}
	if !d.finalizeSessionExit(taskID, exit) {
		return false
	}
	d.publishSessionExit(taskID, exit, nil)
	slog.Info("hera reclaim: recovered observed exit", "task", taskID, "pid", pid)
	return true
}

// retryHeraReclaimStop covers interruption between persisting intent and
// delivering the original request. The supervisor checks the exact PID and
// rejects a stop during a pending restart or if a stop is already in progress.
func (d *Daemon) retryHeraReclaimStop(owner reclaimExitOwner, taskID string, pid int) {
	info, err := owner.ProbeSession(taskID)
	if err != nil || !info.Alive || info.PID != pid {
		slog.Info("hera reclaim: exit recovery awaits retirement", "task", taskID, "pid", pid, "info", info, "err", err)
		return
	}
	unlock, acquired, err := d.db.TryLockHeraReclaim(taskID)
	if err != nil || !acquired {
		slog.Info("hera reclaim: retry lock unavailable", "task", taskID, "err", err)
		return
	}
	defer unlock()
	task, err := d.db.Get(taskID)
	if err != nil || !task.Archived || task.IsResident() {
		slog.Info("hera reclaim: old stop is no longer eligible", "task", taskID, "err", err)
		return
	}
	bindings, err := d.db.ListHeraLiveBindingsByTask(taskID)
	if err != nil || len(bindings) != 0 {
		slog.Info("hera reclaim: retry binding guard", "task", taskID, "live", len(bindings), "err", err)
		return
	}
	if err := owner.StopTreeForPID(taskID, pid); err != nil {
		slog.Info("hera reclaim: retry still unsettled", "task", taskID, "pid", pid, "err", err)
		return
	}
	slog.Info("hera reclaim: interrupted stop accepted", "task", taskID, "pid", pid)
}
