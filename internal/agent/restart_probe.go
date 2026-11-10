package agent

// ProbeRestart provides the REST runtime handler with one local state snapshot.
func (r *Runner) ProbeRestart(taskID string) (live, pending bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, pending = r.pendingRestart[taskID]
	sess := r.sessions[taskID]
	return sess != nil && sess.Alive(), pending, nil
}
