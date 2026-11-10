package daemon

import (
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// Graduation refuses while an account/backend move of the worker is in
// flight: the move could clear the session ID the graduate resumes from.
// The residual cross-lock race is a named follow-up (gotchas/resident.md).
func TestGraduationPreflightRefusesDuringAccountMove(t *testing.T) {
	d, w, _ := graduatedFixture(t)
	r := &graduationRunner{sess: &graduationSession{idle: true, pid: 11}}
	g := newGraduationWatcher(d, r, d.Config, nil)
	g.resumable = func(*model.Task, config.Config) (bool, error) { return true, nil }
	testutil.NoError(t, g.preflight(w))

	agent.HoldAccountMoveForTest(t, w.ID)
	err := g.preflight(w)
	testutil.True(t, err != nil)
	testutil.Contains(t, err.Error(), "move of this worker is in progress")
	testutil.True(t, agent.AccountMoveInFlight(w.ID))
}
