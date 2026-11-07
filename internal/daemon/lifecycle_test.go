package daemon

import (
	"testing"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/testutil"
)

func TestRunnerSessions_NoSession(t *testing.T) {
	rs := runnerSessions{agent.NewRunner(nil)}
	testutil.True(t, !rs.SessionAlive("missing"))
	testutil.True(t, rs.SendSystemInput("missing", "x") != nil)
}

func TestNewHeraLifecycleService(t *testing.T) {
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	svc := NewHeraLifecycleService(d, agent.NewRunner(nil), d.Config)
	testutil.True(t, svc != nil)
}
