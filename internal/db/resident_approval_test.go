package db

import (
	"encoding/json"
	"path/filepath"
	"strings"
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
	for _, name := range []string{"tool", "input", "server", "cwd", "session", "launch", "expiry", "denied"} {
		t.Run(name, func(t *testing.T) {
			d, call, now := approvalFixture(t)
			a, _, err := d.RequestResidentApproval(call, now)
			testutil.NoError(t, err)
			testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, name != "denied", now))
			original := call
			switch name {
			case "tool":
				call.Tool = "mcp__connector__write"
			case "input":
				call.Input = json.RawMessage(`{"command":"echo other"}`)
			case "server":
				call.Server = json.RawMessage(`{"name":"other connector"}`)
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
			if name != "expiry" && name != "denied" {
				_, allow, err = d.RequestResidentApproval(original, now)
				testutil.NoError(t, err)
				testutil.True(t, allow)
			}
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

func TestResidentApprovalBashDescriptionDoesNotChangeGrant(t *testing.T) {
	d, call, now := approvalFixture(t)
	call.Input = json.RawMessage(`{"command":"echo probe","description":"A","timeout":1000,"run_in_background":false}`)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
	notices, err := d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Contains(t, notices[0].Body, `"command":"echo probe"`)
	testutil.Contains(t, notices[0].Body, `"run_in_background":false`)
	for _, input := range []string{`{"command":"echo different","description":"B","timeout":1000,"run_in_background":false}`, `{"command":"echo probe","timeout":1001,"run_in_background":false}`, `{"command":"echo probe","timeout":1000,"run_in_background":true}`} {
		call.Input = json.RawMessage(input)
		_, allow, err := d.RequestResidentApproval(call, now)
		testutil.NoError(t, err)
		testutil.False(t, allow)
	}
	call.Input = json.RawMessage(`{"command":"echo probe","description":"B","timeout":1000,"run_in_background":false}`)
	_, allow, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.True(t, allow)
}

func TestResidentApprovalConsumeRequiresAffectedRow(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
	_, err = d.conn.Exec(`CREATE TRIGGER ignore_consume BEFORE UPDATE OF state ON resident_approvals WHEN NEW.state='consumed' BEGIN SELECT RAISE(IGNORE); END`)
	testutil.NoError(t, err)
	_, allow, err := d.RequestResidentApproval(call, now)
	testutil.Error(t, err)
	testutil.False(t, allow)
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

func TestResidentApprovalDecisionRollback(t *testing.T) {
	d, call, now := approvalFixture(t)
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	_, err = d.conn.Exec(`CREATE TRIGGER fail_approval_notice BEFORE INSERT ON hera_messages BEGIN SELECT RAISE(ABORT,'notice failed'); END`)
	testutil.NoError(t, err)
	testutil.Error(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
	_, allow, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.False(t, allow)
	items, err := d.ListHeraBoardItems(a.RoleID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	_, err = d.conn.Exec(`DROP TRIGGER fail_approval_notice`)
	testutil.NoError(t, err)
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, false, now))
}

func TestResidentApprovalCanonicalNestedData(t *testing.T) {
	canonical, err := canonicalApprovalJSON([]byte(`{"items":[null,true,{"b":2,"a":"x"}],"id":9007199254740993}`))
	testutil.NoError(t, err)
	testutil.Equal(t, string(canonical), `{"id":9007199254740993,"items":[null,true,{"a":"x","b":2}]}`)
	for _, raw := range []string{`{"nested":{"x":1,"x":2}}`, `{"x":[1,]}`, `{"x":[`, strings.Repeat("{\"x\":", 66) + `{}` + strings.Repeat("}", 66), strings.Repeat(" ", 49*1024)} {
		_, err := canonicalApprovalJSON([]byte(raw))
		testutil.Error(t, err)
	}
	_, err = canonicalApprovalJSON([]byte(`{"html":"` + strings.Repeat("<", 9000) + `"}`))
	testutil.Error(t, err)
	_, err = canonicalApprovalJSON([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})
	testutil.Error(t, err)
}

func TestResidentApprovalDurableAcrossHandles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "approval.sqlite")
	d, err := Open(path)
	testutil.NoError(t, err)
	r, _, err := d.EnsureResidentTask(t.TempDir())
	testutil.NoError(t, err)
	_, err = d.EnsureResidentOrchestrator(r.ID)
	testutil.NoError(t, err)
	call := ApprovalCall{TaskID: r.ID, Launch: "launch", Session: "s", Cwd: r.Worktree, Tool: "Bash", Input: json.RawMessage(`{"command":"echo probe"}`)}
	now := time.Now()
	a, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.Close())
	d, err = Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	testutil.NoError(t, d.DecideResidentApproval(a.BoardID, a.CreatedAt, call.Launch, true, now))
	other, err := Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	results := make(chan bool, 2)
	var wg sync.WaitGroup
	for _, handle := range []*DB{d, other} {
		wg.Go(func() {
			_, allow, err := handle.RequestResidentApproval(call, now)
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
	notices, err := other.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(notices), 1)
}
