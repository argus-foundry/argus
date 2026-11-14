package client

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

// The daemon copies the transcript and restarts the session synchronously, so a
// resident move must not use the 2s default RPC timeout.
func TestResidentMoveTimeoutIsGenerous(t *testing.T) {
	testutil.True(t, residentMoveTimeout >= 30*time.Second)
	testutil.True(t, residentMoveTimeout > rpcTimeout)
}
