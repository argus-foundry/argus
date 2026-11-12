package api

import "github.com/argus-foundry/argus/internal/uxlog"

type wtWatchCleanup struct {
	watcher wtWatcher
	units   int
	taskID  string
}

func (b *wtWatchBudget) retain(w wtWatcher, units int, taskID string) {
	if units == 0 {
		return
	}
	b.cleanupMu.Lock()
	defer b.cleanupMu.Unlock()
	b.pending = append(b.pending, wtWatchCleanup{w, units, taskID})
}

func (b *wtWatchBudget) reconcile() {
	b.cleanupMu.Lock()
	defer b.cleanupMu.Unlock()
	pending := b.pending
	b.pending = nil
	for _, cleanup := range pending {
		if err := cleanup.watcher.Close(); err != nil {
			b.pending = append(b.pending, cleanup)
			uxlog.Log("[gitwatch] teardown retry failed task=%s retaining=%d: %v", cleanup.taskID, cleanup.units, err)
			continue
		}
		b.release(cleanup.units)
		uxlog.Log("[gitwatch] teardown recovered task=%s released=%d", cleanup.taskID, cleanup.units)
	}
}
