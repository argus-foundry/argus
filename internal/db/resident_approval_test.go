package db

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func approvalFixture(t *testing.T) (*DB, ApprovalCall, time.Time) {
	t.Helper()
	d := testDB(t)
	r, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	_, err = d.EnsureResidentOrchestrator(r.ID)
	testutil.NoError(t, err)
	return d, ApprovalCall{TaskID: r.ID, Launch: "launch", Session: "session", Cwd: r.Worktree, Tool: "Bash", Input: json.RawMessage(`{"command":"echo probe","timeout":9007199254740993}`)}, time.Now().UTC()
}

func TestResidentApprovalExactGrant(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, allow, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.False(t, allow)
	b, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.Equal(t, a.BoardID, b.BoardID)
	items, err := d.ListHeraBoardItems(a.RoleID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	testutil.True(t, items[0].ResidentApproval)
	testutil.Contains(t, items[0].Body, "9007199254740993")
	_, err = d.UpdateHeraBoardItem(a.RoleID, a.BoardID, "fyi", "different", "changed")
	testutil.Error(t, err)
	testutil.Error(t, d.ResolveHeraBoardItem(a.RoleID, a.BoardID, "agent", "approved"))
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
	testutil.Error(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
	notices, err := d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(notices), 1)
	testutil.Contains(t, notices[0].Body, "[argus] ")
	call.Input = json.RawMessage(`{ "timeout":9007199254740993,"command":"echo probe" }`)
	_, allow, err = d.RequestResidentApproval(call, now.Add(time.Second))
	testutil.NoError(t, err)
	testutil.True(t, allow)
	_, allow, err = d.RequestResidentApproval(call, now.Add(2*time.Second))
	testutil.NoError(t, err)
	testutil.False(t, allow)
}

func TestResidentApprovalMismatchAndExpiry(t *testing.T) {
	for _, name := range []string{"tool", "input", "cwd", "session", "launch", "expiry", "denied"} {
		t.Run(name, func(t *testing.T) {
			d, call, now := approvalFixture(t)
			a, _, err := d.RequestResidentApproval(call, now)
			testutil.NoError(t, err)
			testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, name != "denied", now))
			switch name {
			case "tool":
				call.Tool = "mcp__connector__write"
			case "input":
				call.Input = json.RawMessage(`{"command":"echo other"}`)
			case "cwd":
				call.Cwd += "/other"
			case "session":
				call.Session = "other"
			case "launch":
				call.Launch = "other"
			case "expiry":
				now = now.Add(11 * time.Minute)
			}
			_, allow, err := d.RequestResidentApproval(call, now)
			testutil.NoError(t, err)
			testutil.False(t, allow)
		})
	}
}

func TestResidentApprovalConcurrentConsume(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for range 8 {
		wg.Go(func() {
			_, allow, err := d.RequestResidentApproval(call, now)
			testutil.NoError(t, err)
			results <- allow
		})
	}
	wg.Wait()
	close(results)
	n := 0
	for allow := range results {
		if allow {
			n++
		}
	}
	testutil.Equal(t, n, 1)
}

func TestResidentApprovalRejectsMalformedAndStale(t *testing.T) {
	for _, input := range []string{`{"x":1,"x":2}`, `[]`, `{}`, `null`, `{"x":1} {}`, `{"x":`} {
		t.Run(input, func(t *testing.T) {
			d, call, now := approvalFixture(t)
			call.Input = json.RawMessage(input)
			_, _, err := d.RequestResidentApproval(call, now)
			if input == "{}" {
				testutil.NoError(t, err)
			} else {
				testutil.Error(t, err)
			}
		})
	}
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.Error(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, "other-launch", true, now))
	testutil.Error(t, d.DecideResidentApproval(a.BoardID, now.Add(time.Second), call.Launch, true, now))
	call.TaskID = "ordinary"
	_, _, err = d.RequestResidentApproval(call, now)
	testutil.Error(t, err)
}
