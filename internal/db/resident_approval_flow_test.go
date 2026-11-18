package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

// approvalItem returns the board item for an approval, including resolved ones.
func approvalItem(t *testing.T, d *DB, roleID, id int64) *HeraBoardItem {
	t.Helper()
	items, err := d.ListHeraBoardItems(roleID, true)
	testutil.NoError(t, err)
	for _, i := range items {
		if i.ID == id {
			return i
		}
	}
	t.Fatalf("board item %d not found", id)
	return nil
}

func approvalNotices(t *testing.T, d *DB) []*HeraMessage {
	t.Helper()
	n, err := d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	return n
}

func TestResidentApprovalBoardStateAfterDecision(t *testing.T) {
	for _, approve := range []bool{true, false} {
		t.Run(fmt.Sprintf("approve=%t", approve), func(t *testing.T) {
			d, call, now := approvalFixture(t)
			a, _, err := d.RequestResidentApproval(call, now)
			testutil.NoError(t, err)
			i := approvalItem(t, d, a.RoleID, a.BoardID)
			testutil.False(t, i.Resolved)
			testutil.Equal(t, i.ApprovalState, ApprovalPending)
			testutil.True(t, i.ResidentApproval)

			testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, approve, now))
			i = approvalItem(t, d, a.RoleID, a.BoardID)
			testutil.True(t, i.Resolved)
			testutil.Equal(t, i.ResolvedBy, "human")
			if approve {
				testutil.Equal(t, i.ApprovalState, ApprovalApproved)
				testutil.Contains(t, i.ResolutionNote, "approved ")
				testutil.Contains(t, i.ResolutionNote, "good until")
			} else {
				testutil.Equal(t, i.ApprovalState, ApprovalDenied)
				testutil.Contains(t, i.ResolutionNote, "denied ")
			}
			raw, err := json.Marshal(i)
			testutil.NoError(t, err)
			testutil.Contains(t, string(raw), `"approval_state":"`+i.ApprovalState+`"`)
			testutil.Contains(t, string(raw), `"resolved":true`)
		})
	}
}

func TestResidentApprovalReclickMessages(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, d *DB, call ApprovalCall, a *ResidentApproval, now time.Time) time.Time
		want  []string
	}{
		{"already approved", func(t *testing.T, d *DB, call ApprovalCall, a *ResidentApproval, now time.Time) time.Time {
			testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
			return now.Add(time.Minute)
		}, []string{"already approved at", "waiting for the agent to run it", "good until"}},
		{"already used", func(t *testing.T, d *DB, call ApprovalCall, a *ResidentApproval, now time.Time) time.Time {
			testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
			_, allow, err := d.RequestResidentApproval(call, now.Add(time.Minute))
			testutil.NoError(t, err)
			testutil.True(t, allow)
			return now.Add(2 * time.Minute)
		}, []string{"already approved at", "used: the agent ran it at"}},
		{"denied", func(t *testing.T, d *DB, call ApprovalCall, a *ResidentApproval, now time.Time) time.Time {
			testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, false, now))
			return now
		}, []string{"already denied at"}},
		{"expired", func(t *testing.T, d *DB, call ApprovalCall, a *ResidentApproval, now time.Time) time.Time {
			testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
			later := now.Add(ResidentApprovalWindow + time.Minute)
			_, err := d.SweepResidentApprovals(call.Launch, later)
			testutil.NoError(t, err)
			return later
		}, []string{"expired unused", "re-send"}},
		{"launch changed", func(t *testing.T, d *DB, call ApprovalCall, a *ResidentApproval, now time.Time) time.Time {
			_, err := d.SweepResidentApprovals("new-launch", now)
			testutil.NoError(t, err)
			return now
		}, []string{"lapsed at", "relaunched", "re-send"}},
		{"withdrawn", func(t *testing.T, d *DB, call ApprovalCall, a *ResidentApproval, now time.Time) time.Time {
			testutil.NoError(t, d.ResolveHeraBoardItem(a.RoleID, a.BoardID, "agent", "no longer needed"))
			return now
		}, []string{"withdrawn by the agent"}},
		{"superseded", func(t *testing.T, d *DB, call ApprovalCall, a *ResidentApproval, now time.Time) time.Time {
			call.Launch, call.Session = "new-launch", "new-session"
			b, _, err := d.RequestResidentApproval(call, now)
			testutil.NoError(t, err)
			testutil.True(t, b.BoardID != a.BoardID)
			return now
		}, []string{"superseded by #2", "decide #2 instead"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, call, now := approvalFixture(t)
			a, _, err := d.RequestResidentApproval(call, now)
			testutil.NoError(t, err)
			at := tc.setup(t, d, call, a, now)
			before := len(approvalNotices(t, d))
			err = d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, at)
			testutil.ErrorIs(t, err, ErrResidentApproval)
			var state *ApprovalStateError
			testutil.True(t, errors.As(err, &state))
			for _, w := range tc.want {
				testutil.Contains(t, err.Error(), w)
			}
			testutil.Equal(t, len(approvalNotices(t, d)), before) // a refused click sends nothing
		})
	}
}

func TestResidentApprovalDecideLapsedAndChanged(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	err = d.DecideResidentApproval(a.BoardID, a.CreatedAt, "other-launch", true, now)
	testutil.ErrorIs(t, err, ErrResidentApproval)
	testutil.Contains(t, err.Error(), "relaunched after it was queued")
	err = d.DecideResidentApproval(a.BoardID, now.Add(time.Second), call.Launch, true, now)
	testutil.Contains(t, err.Error(), "changed since you opened it")
	// Denial needs no launch match: the item can always be refused.
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, "other-launch", false, now))
}

func TestResidentApprovalSweepExpiryOnce(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
	base := len(approvalNotices(t, d))

	// Inside the window nothing changes.
	n, err := d.SweepResidentApprovals(call.Launch, now.Add(ResidentApprovalWindow-time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, n, 0)

	later := now.Add(ResidentApprovalWindow + time.Second)
	n, err = d.SweepResidentApprovals(call.Launch, later)
	testutil.NoError(t, err)
	testutil.Equal(t, n, 1)
	i := approvalItem(t, d, a.RoleID, a.BoardID)
	testutil.Equal(t, i.ApprovalState, ApprovalExpired)
	testutil.True(t, i.Resolved)
	testutil.Contains(t, i.ResolutionNote, "expired unused")
	notices := approvalNotices(t, d)
	testutil.Equal(t, len(notices), base+1)
	last := notices[len(notices)-1]
	testutil.Contains(t, last.Body, "[argus] Deferred call #")
	testutil.Contains(t, last.Body, "nothing ran")
	testutil.Contains(t, last.Body, "Stored canonical call:")
	testutil.Contains(t, last.Body, "echo probe")

	// Announced once: a later sweep finds nothing.
	n, err = d.SweepResidentApprovals(call.Launch, later.Add(time.Hour))
	testutil.NoError(t, err)
	testutil.Equal(t, n, 0)
	testutil.Equal(t, len(approvalNotices(t, d)), base+1)

	// The expired grant never authorizes a retry.
	_, allow, err := d.RequestResidentApproval(call, later)
	testutil.NoError(t, err)
	testutil.False(t, allow)
}

func TestResidentApprovalSweepLaunchChange(t *testing.T) {
	for _, approved := range []bool{false, true} {
		t.Run(fmt.Sprintf("approved=%t", approved), func(t *testing.T) {
			d, call, now := approvalFixture(t)
			a, _, err := d.RequestResidentApproval(call, now)
			testutil.NoError(t, err)
			if approved {
				testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
			}
			base := len(approvalNotices(t, d))

			// The current launch and an unknown launch never lapse a fresh item.
			for _, launch := range []string{call.Launch, ""} {
				n, err := d.SweepResidentApprovals(launch, now)
				testutil.NoError(t, err)
				testutil.Equal(t, n, 0)
			}
			n, err := d.SweepResidentApprovals("relaunched", now)
			testutil.NoError(t, err)
			testutil.Equal(t, n, 1)
			i := approvalItem(t, d, a.RoleID, a.BoardID)
			testutil.Equal(t, i.ApprovalState, ApprovalStale)
			testutil.True(t, i.Resolved)
			testutil.Equal(t, i.ResolvedBy, "argus")
			testutil.Contains(t, i.ResolutionNote, "relaunched before use")
			notices := approvalNotices(t, d)
			testutil.Equal(t, len(notices), base+1)
			testutil.Contains(t, notices[len(notices)-1].Body, "relaunched before it was used")
			testutil.Contains(t, notices[len(notices)-1].Body, "re-send the exact call")

			// A stale grant never authorizes, even on its original launch.
			_, allow, err := d.RequestResidentApproval(call, now)
			testutil.NoError(t, err)
			testutil.False(t, allow)
		})
	}
}

func TestResidentApprovalSupersedeOnRequeue(t *testing.T) {
	d, call, now := approvalFixture(t)
	first, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.False(t, first.Reused)

	again, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.True(t, again.Reused)
	testutil.Equal(t, again.BoardID, first.BoardID)
	testutil.Equal(t, len(again.Superseded), 0)

	// Same content on a new launch and session (relaunch or /clear).
	call.Launch, call.Session = "launch-2", "session-2"
	second, _, err := d.RequestResidentApproval(call, now.Add(time.Minute))
	testutil.NoError(t, err)
	testutil.False(t, second.Reused)
	testutil.DeepEqual(t, second.Superseded, []int64{first.BoardID})
	old := approvalItem(t, d, first.RoleID, first.BoardID)
	testutil.Equal(t, old.ApprovalState, ApprovalSuperseded)
	testutil.True(t, old.Resolved)
	testutil.Equal(t, old.ResolutionNote, fmt.Sprintf("superseded by #%d", second.BoardID))
	open, err := d.ListHeraBoardItems(first.RoleID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(open), 1)
	testutil.Equal(t, open[0].ID, second.BoardID)

	// Different content never supersedes.
	call.Input = json.RawMessage(`{"command":"echo other"}`)
	third, _, err := d.RequestResidentApproval(call, now.Add(2*time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, len(third.Superseded), 0)

	// An approved grant is never superseded: approve the second, re-queue
	// its content on yet another launch, and the grant row stays approved
	// until the sweep lapses it.
	call.Input = json.RawMessage(`{"command":"echo probe","timeout":9007199254740993}`)
	testutil.NoError(t, d.DecideResidentApproval(second.BoardID, second.CreatedAt, "launch-2", true, now))
	call.Launch = "launch-3"
	fourth, _, err := d.RequestResidentApproval(call, now.Add(3*time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, len(fourth.Superseded), 0)
	testutil.Equal(t, approvalItem(t, d, first.RoleID, second.BoardID).ApprovalState, ApprovalApproved)
}

func TestResidentApprovalAgentWithdraw(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	base := len(approvalNotices(t, d))
	testutil.NoError(t, d.ResolveHeraBoardItem(a.RoleID, a.BoardID, "agent", "superseded"))
	i := approvalItem(t, d, a.RoleID, a.BoardID)
	testutil.Equal(t, i.ApprovalState, ApprovalWithdrawn)
	testutil.True(t, i.Resolved)
	testutil.Equal(t, i.ResolvedBy, "agent")
	testutil.Equal(t, i.ResolutionNote, "withdrawn by agent: superseded")
	testutil.Equal(t, len(approvalNotices(t, d)), base) // no self-notice
	// Withdrawing never grants.
	_, allow, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.False(t, allow)

	// Withdrawing a decided item reports its state and leaves the grant.
	b, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.DecideResidentApproval(b.BoardID, b.CreatedAt, call.Launch, true, now))
	err = d.ResolveHeraBoardItem(b.RoleID, b.BoardID, "agent", "")
	testutil.ErrorIs(t, err, ErrResidentApproval)
	testutil.Contains(t, err.Error(), "already approved at")
	testutil.Equal(t, approvalItem(t, d, b.RoleID, b.BoardID).ApprovalState, ApprovalApproved)

	// A human cannot dismiss an approval item through the generic path.
	c, _, err := d.RequestResidentApproval(ApprovalCall{TaskID: call.TaskID, Launch: call.Launch, Session: call.Session, Cwd: call.Cwd, Tool: "Bash", Input: json.RawMessage(`{"command":"echo third"}`)}, now)
	testutil.NoError(t, err)
	err = d.ResolveHeraBoardItem(c.RoleID, c.BoardID, "human", "")
	testutil.ErrorIs(t, err, ErrResidentApproval)
	testutil.Contains(t, err.Error(), "use approve or deny")
	testutil.Equal(t, approvalItem(t, d, c.RoleID, c.BoardID).ApprovalState, ApprovalPending)
}

func TestResidentApprovalConsumeNotesRunTime(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
	notices := approvalNotices(t, d)
	testutil.Contains(t, notices[len(notices)-1].Body, "within 60 minutes")
	_, allow, err := d.RequestResidentApproval(call, now.Add(30*time.Minute))
	testutil.NoError(t, err)
	testutil.True(t, allow)
	i := approvalItem(t, d, a.RoleID, a.BoardID)
	testutil.Equal(t, i.ApprovalState, ApprovalConsumed)
	testutil.Contains(t, i.ResolutionNote, "ran ")
	testutil.Contains(t, i.Body, "does not time out")
}

func TestPostOrRefreshHeraBoardItem(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	role := a.RoleID

	first, err := d.PostOrRefreshHeraBoardItem(role, "fyi", "Solo moved accounts: default to personal", "first")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ReadHeraBoardItem(role, first.ID))
	second, err := d.PostOrRefreshHeraBoardItem(role, "fyi", "Solo moved accounts: default to personal", "second")
	testutil.NoError(t, err)
	testutil.Equal(t, second.ID, first.ID)
	testutil.Equal(t, second.Body, "second")
	testutil.False(t, second.Read)

	// A different kind or subject is its own item.
	other, err := d.PostOrRefreshHeraBoardItem(role, "decision", "Solo moved accounts: default to personal", "x")
	testutil.NoError(t, err)
	testutil.True(t, other.ID != first.ID)

	// Once resolved, the next notice is a new item.
	testutil.NoError(t, d.ResolveHeraBoardItem(role, first.ID, "human", ""))
	third, err := d.PostOrRefreshHeraBoardItem(role, "fyi", "Solo moved accounts: default to personal", "third")
	testutil.NoError(t, err)
	testutil.True(t, third.ID != first.ID)

	// An approval item is never rewritten by a notice with its subject.
	byApproval, err := d.PostOrRefreshHeraBoardItem(role, "decision", "Approve call: Bash", "spoof")
	testutil.NoError(t, err)
	testutil.True(t, byApproval.ID != a.BoardID)
	testutil.Equal(t, approvalItem(t, d, role, a.BoardID).ApprovalState, ApprovalPending)

	_, err = d.PostOrRefreshHeraBoardItem(role, "bogus", "s", "b")
	testutil.ErrorIs(t, err, ErrHeraBoardInvalid)
}
