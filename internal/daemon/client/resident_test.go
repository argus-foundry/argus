package client

import (
	"testing"

	"github.com/argus-foundry/argus/internal/daemon"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCliResident(t *testing.T) {
	fd := newFakeDaemon(t)
	fd.resident = daemon.ResidentStateResp{Enabled: true, Name: "Solo", TaskID: "resident", Unread: true}
	c := fakeClient(t, fd)
	state, err := c.ResidentState()
	testutil.NoError(t, err)
	testutil.DeepEqual(t, state, fd.resident)
	testutil.NoError(t, c.ResidentMarkViewed())
	testutil.NoError(t, c.ResidentSend("Hello", "id-1"))
	fd.mu.Lock()
	testutil.True(t, fd.residentViewed)
	testutil.DeepEqual(t, fd.residentSent, daemon.ResidentSendReq{Text: "Hello", DeliveryID: "id-1"})
	fd.residentError = "disabled"
	fd.mu.Unlock()
	testutil.Error(t, c.ResidentMarkViewed())
	testutil.Error(t, c.ResidentSend("Hello", ""))
	_ = c.Close()
	_, err = c.ResidentState()
	testutil.Error(t, err)
	testutil.Error(t, c.ResidentMarkViewed())
	testutil.Error(t, c.ResidentSend("Hello", ""))
}
