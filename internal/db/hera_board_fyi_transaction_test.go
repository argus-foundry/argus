package db

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardFYIRollback(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"item", `CREATE TRIGGER reject_fyi BEFORE UPDATE ON hera_board_items BEGIN SELECT RAISE(ABORT,'failed'); END`},
		{"receipt", `CREATE TRIGGER reject_fyi BEFORE INSERT ON hera_board_fyi_receipts BEGIN SELECT RAISE(ABORT,'failed'); END`},
		{"notice", `CREATE TRIGGER reject_fyi BEFORE INSERT ON hera_messages BEGIN SELECT RAISE(ABORT,'failed'); END`},
		{"commit", `CREATE TABLE commit_guard(role_id INTEGER REFERENCES hera_roles(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER reject_fyi AFTER INSERT ON hera_messages BEGIN INSERT INTO commit_guard VALUES(-1); END`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, c, i := fyiFixture(t)
			_, err := d.conn.Exec(tc.sql)
			testutil.NoError(t, err)
			sink := sinkFor(t)
			_, err = d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{{ActionID: "a", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment_and_dismiss", Note: "feedback"}})
			testutil.Error(t, err)
			testutil.Equal(t, len(sink.board()), 0)
			items, err := d.ListHeraBoardItems(c.ID, true)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, items[0], i)
			_, err = d.HeraBoardFYIReceipt(c.ID, "a")
			testutil.ErrorIs(t, err, ErrHeraBoardNotFound)
			messages, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(messages), 0)
		})
	}
}

func TestBoardFYIReplaySnapshotAndChangedPayload(t *testing.T) {
	d, c, i := fyiFixture(t)
	a := HeraBoardFYIAction{ActionID: "a", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: " Original \n"}
	original, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
	testutil.NoError(t, err)
	_, err = d.UpdateHeraBoardItem(c.ID, i.ID, "fyi", "Changed subject", "changed body")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "agent", "done"))
	// The same instant in another timezone has the same normalized fingerprint.
	a.ExpectedUpdatedAt = a.ExpectedUpdatedAt.In(time.FixedZone("offset", 3600))
	replayed, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
	testutil.NoError(t, err)
	testutil.DeepEqual(t, replayed, original)
	for _, field := range []string{"item", "revision", "action", "note"} {
		t.Run(field, func(t *testing.T) {
			changed := a
			switch field {
			case "item":
				changed.ItemID++
			case "revision":
				changed.ExpectedUpdatedAt = changed.ExpectedUpdatedAt.Add(time.Second)
			case "action":
				changed.Action = "comment_and_dismiss"
			case "note":
				changed.Note = "different"
			}
			result, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{changed})
			testutil.NoError(t, err)
			testutil.Equal(t, result[0].Outcome, "invalid")
			testutil.Nil(t, result[0].Item)
		})
	}
	receipt, err := d.HeraBoardFYIReceipt(c.ID, "a")
	testutil.NoError(t, err)
	testutil.Equal(t, receipt.Note, a.Note)
	messages, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(messages), 1)
}

func TestBoardFYIMixedBatchAndOutbox(t *testing.T) {
	d, c, one := fyiFixture(t)
	var items []*HeraBoardItem
	items = append(items, one)
	for _, subject := range []string{"combined", "silent", "conflicted", "invalid"} {
		i, err := d.PostHeraBoardItem(c.ID, "fyi", subject, "body")
		testutil.NoError(t, err)
		items = append(items, i)
	}
	decision, err := d.PostHeraBoardItem(c.ID, "decision", "Still decide", "body")
	testutil.NoError(t, err)
	var actions []HeraBoardFYIAction
	for n, i := range items {
		a := HeraBoardFYIAction{ActionID: fmt.Sprintf("a%d", n), ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: fmt.Sprintf("note%d", n)}
		switch n {
		case 1:
			a.Action = "comment_and_dismiss"
		case 2:
			a.Action, a.Note = "dismiss", ""
		case 3:
			a.ExpectedUpdatedAt = i.UpdatedAt.Add(-time.Second)
		case 4:
			a.Note = " "
		}
		actions = append([]HeraBoardFYIAction{a}, actions...)
	}
	seen := 0
	prev := events.SetSink(boardObservingSink{observe: func(e model.Event) {
		seen++
		// Reenter the DB: events must follow commit and lock release.
		r, err := d.HeraBoardFYIReceipt(c.ID, "a1")
		testutil.NoError(t, err)
		testutil.Equal(t, r.Item.Resolved, true)
		messages, err := d.HeraInbox(c.ID)
		testutil.NoError(t, err)
		testutil.Equal(t, len(messages), 1)
	}})
	t.Cleanup(func() { events.SetSink(prev) })
	results, err := d.FYIHeraBoardItems(c.ID, actions)
	testutil.NoError(t, err)
	for n, want := range []string{"invalid", "conflict", "resolved", "resolved", "commented"} {
		testutil.Equal(t, results[n].Outcome, want)
	}
	testutil.Equal(t, seen, 2)
	_, err = d.FYIHeraBoardItems(c.ID, actions)
	testutil.NoError(t, err)
	testutil.Equal(t, seen, 2)
	pending, err := d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 0)
	_, err = d.CreateHeraBinding(CreateHeraBindingInput{RoleID: c.ID, ArgusTaskID: "task", WorktreePath: "/wt"})
	testutil.NoError(t, err)
	pending, err = d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 1)
	body := pending[0].Body
	testutil.Equal(t, pending[0].FromRoleID, c.ID)
	testutil.Equal(t, pending[0].ToRoleID, c.ID)
	testutil.Equal(t, strings.Index(body, "note0") < strings.Index(body, "note1"), true)
	testutil.Contains(t, body, "State: open; comment only")
	testutil.Contains(t, body, "State: already resolved; comment and dismiss")
	testutil.Equal(t, strings.Contains(body, "silent"), false)
	testutil.Contains(t, body, fmt.Sprintf("Still open decisions: #%d", decision.ID))
	testutil.Contains(t, body, fmt.Sprintf("Still open FYIs: #%d, #%d, #%d", one.ID, items[3].ID, items[4].ID))
	testutil.NoError(t, d.MarkHeraMessageDelivered(pending[0].ID, HeraDeliveryIdleSubmit))
	pending, err = d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 0)
}

func TestBoardFYIResidentApproval(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, call, now := approvalFixture(t)
	approval, _, err := d.RequestResidentApproval(call, now)
	testutil.NoError(t, err)
	items, err := d.ListHeraBoardItems(approval.RoleID, false)
	testutil.NoError(t, err)
	i := items[0]
	results, err := d.FYIHeraBoardItems(approval.RoleID, []HeraBoardFYIAction{{ActionID: "a", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment_and_dismiss", Note: "no"}})
	testutil.NoError(t, err)
	testutil.Equal(t, results[0].Outcome, "invalid")
}

func TestBoardFYIPartialAcrossCoordinators(t *testing.T) {
	d := heraTestDB(t)
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			o := mkOrch(t, d, name)
			c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
			fresh, err := d.PostHeraBoardItem(c.ID, "fyi", "Unchanged", "body")
			testutil.NoError(t, err)
			stale, err := d.PostHeraBoardItem(c.ID, "fyi", "Changed", "body")
			testutil.NoError(t, err)
			actions := []HeraBoardFYIAction{
				{ActionID: name + "-fresh", ItemID: fresh.ID, ExpectedUpdatedAt: fresh.UpdatedAt, Action: "comment", Note: "Keep this feedback"},
				{ActionID: name + "-stale", ItemID: stale.ID, ExpectedUpdatedAt: stale.UpdatedAt, Action: "comment", Note: "Keep this note too"},
			}
			updated, err := d.UpdateHeraBoardItem(c.ID, stale.ID, "fyi", stale.Subject, "changed body")
			testutil.NoError(t, err)
			results, err := d.FYIHeraBoardItems(c.ID, actions)
			testutil.NoError(t, err)
			testutil.Equal(t, results[0].Outcome, "commented")
			testutil.Equal(t, results[1].Outcome, "conflict")
			testutil.Equal(t, results[1].Item.UpdatedAt, updated.UpdatedAt)
			// An uncertain whole-batch retry replays the successful sibling.
			replayed, err := d.FYIHeraBoardItems(c.ID, actions)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, replayed, results)
			actions[1].ActionID += "-retry"
			actions[1].ExpectedUpdatedAt = updated.UpdatedAt
			retry, err := d.FYIHeraBoardItems(c.ID, actions[1:])
			testutil.NoError(t, err)
			testutil.Equal(t, retry[0].Outcome, "commented")
			messages, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(messages), 2)
		})
	}
}

func TestBoardClosedFeedbackContext(t *testing.T) {
	for _, kind := range []string{"fyi", "decision"} {
		t.Run(kind, func(t *testing.T) {
			d, c, i := fyiFixture(t)
			var err error
			if kind == "decision" {
				i, err = d.UpdateHeraBoardItem(c.ID, i.ID, kind, i.Subject, i.Body)
				testutil.NoError(t, err)
			}
			testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "agent", "done"))
			if kind == "fyi" {
				r, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{{ActionID: "closed", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: "Feedback"}})
				testutil.NoError(t, err)
				testutil.Equal(t, r[0].Outcome, "invalid")
				testutil.NotNil(t, r[0].Item)
				if r[0].Item != nil {
					testutil.Equal(t, r[0].Item.Resolved, true)
				}
			} else {
				r, err := d.AnswerHeraBoardItems(c.ID, []HeraBoardAnswer{{ActionID: "closed", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: "Feedback"}})
				testutil.NoError(t, err)
				testutil.Equal(t, r[0].Outcome, "invalid")
				testutil.NotNil(t, r[0].Item)
				if r[0].Item != nil {
					testutil.Equal(t, r[0].Item.Resolved, true)
				}
			}
		})
	}
}
