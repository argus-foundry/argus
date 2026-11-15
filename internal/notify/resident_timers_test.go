package notify

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

// Test the actual timer envelope against the notifier's idle gate and dedup.
func TestResidentTimerIdleAndAbsentGating(t *testing.T) {
	r := newFakeRunner()
	n := newTestNotifier(r, fakeNoFocus{})
	line := "[argus] Timer #7 (set at 2026-11-14T12:00:00Z; was due at 2026-11-14T13:00:00Z):\ncheck Sam"
	n.ReliableNotify("solo", line, "resident-timer:fire-7", NotifyOpts{})
	n.Reconcile(time.Now())
	testutil.False(t, n.Submitted("solo", "resident-timer:fire-7"))
	sess := r.addSession("solo", false)
	n.Reconcile(time.Now())
	testutil.Equal(t, len(sess.allWrites()), 0)
	sess.mu.Lock()
	sess.idle = true
	sess.mu.Unlock()
	n.Reconcile(time.Now())
	testutil.True(t, n.Submitted("solo", "resident-timer:fire-7"))
	before := len(sess.allWrites())
	testutil.True(t, before > 0)
	n.ReliableNotify("solo", line, "resident-timer:fire-7", NotifyOpts{})
	n.Reconcile(time.Now())
	testutil.Equal(t, len(sess.allWrites()), before)
}
