package daemon

import "log/slog"

type reclaimExitOwner interface {
	sessionProber
	CheckReclaimSessionRetired(taskID string) error
	ObservedSessionExit(taskID string, pid int) ExitInfo
}

// recoverHeraReclaimExits settles exact supervisor exits that the daemon's
// stream relay missed. The same task lock protects normal exit finalization.
func (d *Daemon) recoverHeraReclaimExits() int {
	owner, ok := d.supClient.(reclaimExitOwner)
	if !ok {
		return 0
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
		slog.Info("hera reclaim: exit recovery awaits retirement", "task", taskID, "pid", pid, "err", err)
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
