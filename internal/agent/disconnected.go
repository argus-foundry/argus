package agent

// DisconnectedGraceTicks is how many CONSECUTIVE ticks a candidate task must be
// absent from the daemon's alive set before it counts as disconnected. It keeps
// a daemon bounce or a session mid-restart from flashing the glyph.
const DisconnectedGraceTicks = 2

// DisconnectedIDs is the reusable "session not running" predicate (see
// openspec show-disconnected-agents D1). candidates are the tasks that SHOULD
// have a session; running is the daemon's alive set (runner.RunningAndIdle).
// prev is the per-task absent-tick counter returned by the previous call.
//
// Fail open: when healthy is false (the session-list read failed, so `running`
// is meaningless) the counters are left untouched and no task is newly marked;
// tasks already past the grace keep their state so the glyph does not flicker.
// Returns the disconnected task IDs and the counter state for the next call.
func DisconnectedIDs(candidates, running []string, healthy bool, prev map[string]int) (ids []string, next map[string]int) {
	if !healthy {
		for _, id := range candidates {
			if prev[id] >= DisconnectedGraceTicks {
				ids = append(ids, id)
			}
		}
		return ids, prev
	}
	alive := make(map[string]bool, len(running))
	for _, id := range running {
		alive[id] = true
	}
	next = make(map[string]int, len(candidates))
	for _, id := range candidates {
		if alive[id] {
			continue
		}
		n := min(prev[id]+1, DisconnectedGraceTicks)
		next[id] = n
		if n >= DisconnectedGraceTicks {
			ids = append(ids, id)
		}
	}
	return ids, next
}
