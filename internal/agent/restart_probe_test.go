package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestProbeRestart(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		session, alive, pending bool
	}{
		{name: "absent"}, {name: "live", session: true, alive: true}, {name: "gap", pending: true},
		{name: "stopping", session: true, alive: true, pending: true}, {name: "dead", session: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRunner(nil)
			if tc.session {
				done := make(chan struct{})
				if !tc.alive {
					close(done)
				}
				r.sessions["task"] = &Session{done: done}
			}
			if tc.pending {
				r.pendingRestart["task"] = &pendingRestart{}
			}
			live, pending, err := r.ProbeRestart("task")
			testutil.NoError(t, err)
			testutil.Equal(t, live, tc.alive)
			testutil.Equal(t, pending, tc.pending)
		})
	}
}
