package agent

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestAccountMoveInFlightAndTestHold(t *testing.T) {
	testutil.False(t, AccountMoveInFlight("hold-1"))
	t.Run("hold", func(t *testing.T) {
		HoldAccountMoveForTest(t, "hold-1")
		testutil.True(t, AccountMoveInFlight("hold-1"))
		testutil.False(t, claimAccountMove("hold-1"))
	})
	testutil.False(t, AccountMoveInFlight("hold-1"))

	prev := sandboxSeamInTest
	sandboxSeamInTest = func() bool { return false }
	defer func() { sandboxSeamInTest = prev }()
	defer func() { testutil.True(t, recover() != nil) }()
	HoldAccountMoveForTest(t, "hold-2")
}

// The move must be claimed in flight BEFORE the pending-graduate check runs,
// so a concurrent graduation preflight (refusing while AccountMoveInFlight)
// sees it. Reordering check-above-claim would leave the window open.
func TestMove_ClaimedBeforePendingGraduateCheck(t *testing.T) {
	for _, tc := range []struct{ name, backend string }{{"account", ""}, {"backend", "codex"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMoveFixture(t, true)
			orig := refuseMoveOfPendingGraduate
			defer func() { refuseMoveOfPendingGraduate = orig }()
			seen, called := false, false
			refuseMoveOfPendingGraduate = func(d *db.DB, id string) error {
				called = true
				seen = AccountMoveInFlight(id)
				return orig(d, id)
			}
			r := &moveRunner{sess: &moveSession{idle: true, alive: true}}
			_, _, _ = MoveTaskAccount(f.d, r, AccountMoveInput{TaskID: "mv", Account: "work", Backend: tc.backend})
			testutil.True(t, called)
			testutil.True(t, seen)
			testutil.False(t, AccountMoveInFlight("mv"))
		})
	}
}
