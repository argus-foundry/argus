package tui

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

// The forced re-render hotkey on a pending graduate is dropped (the daemon
// watcher owns its restart) but must tell the user why; the automatic kick
// stays silent.
func TestPendingGraduate_ForcedRerenderShowsNotice(t *testing.T) {
	for _, tc := range []struct {
		name   string
		forced bool
		notice bool
	}{{"forced", true, true}, {"automatic", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			app, d, task, _ := pendingGraduateFixture(t)
			sess := resumableGraduate(t, app, d, task)
			_, stop := wireApp(t, app)
			defer stop()
			readUI(t, app.tapp, func() {
				app.kickRerenderCore(task, sess, 40, nil, func() bool { return false }, tc.forced)
			})
			time.Sleep(100 * time.Millisecond)
			testutil.False(t, sess.stopCalled.Load())
			if tc.notice {
				testutil.Contains(t, app.header.Notice(), "graduation restart pending")
			} else {
				testutil.Equal(t, app.header.Notice(), "")
			}
		})
	}
}
