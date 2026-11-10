package daemon

import (
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/notify"
	"github.com/argus-foundry/argus/internal/testutil"
)

// TestResidentNotice_ProductionDoorbellWiring builds the manager through the
// Daemon's real residentMgr() (not a test-set ringNotice seam) and asserts a
// human nuke notice rings the resident coordinator's pane via the daemon's
// notifier. Deleting the production `m.ringNotice = ...Deliver(msg)` wiring
// leaves the message committed but unrung, failing the DeliveryState assertion.
func TestResidentNotice_ProductionDoorbellWiring(t *testing.T) {
	e := newNoticeEnv(t)
	notifier := notify.New(&fakeNudgeRunner{}, noFocus{})
	d := &Daemon{db: e.db, notifier: notifier, sessionCore: newSessionCore(agent.NewRunner(nil), func() config.Config { return e.cfg }, make(chan struct{}))}
	m := d.residentMgr()
	testutil.True(t, m.ringNotice != nil)

	testutil.NoError(t, e.db.NukeHeraRole(e.worker.ID))
	m.deliverNotices()

	msgs := e.inbox(t)
	testutil.Equal(t, len(msgs), 1)
	binding, err := e.db.HeraLiveBindingByRole(e.coord.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, notifier.DeliveryState(binding.ArgusTaskID, fmt.Sprintf("hera:%d", msgs[0].ID)), notify.StatePending)
}
