package agent

// HasLiveOrStartingProcess checks whether a process can still write output.
// A drained PTY waiting for its exit callback remains registered but is no
// longer active. Prelaunch and scheduled successors still count as active.
func (r *Runner) HasLiveOrStartingProcess(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, reserved := r.sessions[taskID]
	return r.pendingRestart[taskID] != nil || (reserved && (sess == nil || sess.Alive()))
}
