package db

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/testutil"
)

func fyiFixture(t *testing.T) (*DB, *HeraRole, *HeraBoardItem) {
	t.Helper()
	d := heraTestDB(t)
	o := mkOrch(t, d, "fyi")
	c := mkRole(t, d, o.ID, "coord", HeraKindCoordinator)
	i, err := d.PostHeraBoardItem(c.ID, "fyi", "Status", "Body")
	testutil.NoError(t, err)
	return d, c, i
}

func TestBoardFYIActions(t *testing.T) {
	for _, action := range []string{"dismiss", "comment", "comment_and_dismiss"} {
		t.Run(action, func(t *testing.T) {
			d, c, i := fyiFixture(t)
			testutil.NoError(t, d.ReadHeraBoardItem(c.ID, i.ID))
			note, outcome := "Full feedback", "resolved"
			if action == "dismiss" {
				note = ""
			}
			if action == "comment" {
				outcome = "commented"
			}
			a := HeraBoardFYIAction{ActionID: "action-1", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: action, Note: note}
			sink := sinkFor(t)
			results, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
			testutil.NoError(t, err)
			testutil.Equal(t, results[0].Outcome, outcome)
			testutil.Equal(t, results[0].Item.Read, true)
			items, err := d.ListHeraBoardItems(c.ID, true)
			testutil.NoError(t, err)
			testutil.Equal(t, items[0].Resolved, action != "comment")
			testutil.Equal(t, items[0].ResolutionChoice, "")
			if action == "comment" {
				testutil.Equal(t, items[0].UpdatedAt.Equal(i.UpdatedAt), true)
				testutil.Equal(t, len(sink.board()), 0)
			} else {
				testutil.Equal(t, items[0].UpdatedAt.After(i.UpdatedAt), true)
				testutil.Equal(t, items[0].ResolvedBy, "human")
				testutil.Equal(t, items[0].ResolutionNote, note)
				testutil.Equal(t, len(sink.board()), 1)
			}
			receipt, err := d.HeraBoardFYIReceipt(c.ID, a.ActionID)
			testutil.NoError(t, err)
			testutil.Equal(t, receipt.Note, note)
			testutil.Equal(t, receipt.Outcome, outcome)
			_, err = d.HeraBoardFYIReceipt(c.ID+1, a.ActionID)
			testutil.ErrorIs(t, err, ErrHeraBoardNotFound)
			replay, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
			testutil.NoError(t, err)
			testutil.DeepEqual(t, replay, results)
			msgs, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			want := 1
			if action == "dismiss" {
				want = 0
			}
			testutil.Equal(t, len(msgs), want)
			if want == 1 {
				testutil.Contains(t, msgs[0].Body, "Aaron (board)")
			}
		})
	}
}

func TestBoardFYIOutcomes(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"conflict", "conflict"}, {"wrong role", "not_found"}, {"missing", "not_found"}, {"decision", "not_found"},
		{"unknown action", "invalid"}, {"empty comment", "invalid"}, {"closed comment", "invalid"},
		{"closed dismiss", "already_resolved"}, {"closed combined", "already_resolved"}, {"zero revision", "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, c, i := fyiFixture(t)
			a := HeraBoardFYIAction{ActionID: "a", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: "hello"}
			role := c.ID
			switch tc.name {
			case "conflict":
				a.ExpectedUpdatedAt = i.UpdatedAt.Add(-time.Second)
			case "wrong role":
				role++
			case "missing":
				a.ItemID++
			case "decision":
				_, err := d.conn.Exec(`UPDATE hera_board_items SET kind='decision' WHERE id=?`, i.ID)
				testutil.NoError(t, err)
			case "unknown action":
				a.Action = "nope"
			case "empty comment":
				a.Note = " \n "
			case "closed comment", "closed dismiss", "closed combined":
				testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "agent", ""))
				if tc.name == "closed dismiss" {
					a.Action, a.Note = "dismiss", ""
				}
				if tc.name == "closed combined" {
					a.Action = "comment_and_dismiss"
				}
			case "zero revision":
				a.ExpectedUpdatedAt = time.Time{}
			}
			results, err := d.FYIHeraBoardItems(role, []HeraBoardFYIAction{a})
			testutil.NoError(t, err)
			testutil.Equal(t, results[0].Outcome, tc.want)
			if tc.want == "conflict" {
				testutil.Equal(t, results[0].Item.Body, "Body")
			} else {
				testutil.Nil(t, results[0].Item)
			}
			_, err = d.HeraBoardFYIReceipt(role, "a")
			testutil.ErrorIs(t, err, ErrHeraBoardNotFound)
			messages, err := d.HeraInbox(c.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, len(messages), 0)
		})
	}
}

func TestBoardFYIEnvelope(t *testing.T) {
	for _, kind := range []string{"empty", "too many", "duplicate action", "duplicate item", "bad id", "long id", "long note", "bad utf8", "dismiss note"} {
		t.Run(kind, func(t *testing.T) {
			d, c, i := fyiFixture(t)
			a := HeraBoardFYIAction{ActionID: "a", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: "hello"}
			actions := []HeraBoardFYIAction{a}
			switch kind {
			case "empty":
				actions = nil
			case "too many":
				actions = make([]HeraBoardFYIAction, 17)
			case "duplicate action":
				a.ItemID++
				actions = append(actions, a)
			case "duplicate item":
				a.ActionID = "b"
				actions = append(actions, a)
			case "bad id":
				actions[0].ActionID = "bad/id"
			case "long id":
				actions[0].ActionID = strings.Repeat("a", 129)
			case "long note":
				actions[0].Note = strings.Repeat("x", 4097)
			case "bad utf8":
				actions[0].Note = string([]byte{255})
			case "dismiss note":
				actions[0].Action = "dismiss"
			}
			_, err := d.FYIHeraBoardItems(c.ID, actions)
			testutil.ErrorIs(t, err, ErrHeraBoardInvalid)
			items, err := d.ListHeraBoardItems(c.ID, false)
			testutil.NoError(t, err)
			testutil.Equal(t, len(items), 1)
		})
	}
}

func TestBoardFYISummaryBound(t *testing.T) {
	d, c, first := fyiFixture(t)
	var actions []HeraBoardFYIAction
	for n := range 16 {
		i := first
		if n != 0 {
			var err error
			i, err = d.PostHeraBoardItem(c.ID, "fyi", strings.Repeat("x", 240), "body")
			testutil.NoError(t, err)
		}
		actions = append(actions, HeraBoardFYIAction{ActionID: fmt.Sprintf("action-%d", n), ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: strings.Repeat("界", 1365) + "x"})
	}
	_, err := d.FYIHeraBoardItems(c.ID, actions)
	testutil.NoError(t, err)
	messages, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(messages), 1)
	testutil.Equal(t, len(messages[0].Body) <= 65536, true)
	testutil.Equal(t, utf8.ValidString(messages[0].Body), true)
	for _, a := range actions {
		testutil.Contains(t, messages[0].Body, fmt.Sprintf("role %d action %s", c.ID, a.ActionID))
		r, err := d.HeraBoardFYIReceipt(c.ID, a.ActionID)
		testutil.NoError(t, err)
		testutil.Equal(t, r.Note, a.Note)
	}
}

func TestBoardOpenCounts(t *testing.T) {
	d, c, i := fyiFixture(t)
	testutil.NoError(t, d.ReadHeraBoardItem(c.ID, i.ID))
	_, err := d.PostHeraBoardItem(c.ID, "decision", "Decide", "body")
	testutil.NoError(t, err)
	closed, err := d.PostHeraBoardItem(c.ID, "fyi", "Closed", "body")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, closed.ID, "agent", ""))
	other := mkRole(t, d, c.OrchestratorID, "other", HeraKindCoordinator)
	_, err = d.PostHeraBoardItem(other.ID, "decision", "Other", "body")
	testutil.NoError(t, err)
	before, err := d.ListHeraBoardItems(c.ID, true)
	testutil.NoError(t, err)
	counts, err := d.HeraBoardOpenCounts()
	testutil.NoError(t, err)
	testutil.DeepEqual(t, counts, map[int64]HeraBoardKindCounts{c.ID: {Decisions: 1, FYIs: 1}, other.ID: {Decisions: 1}})
	after, err := d.ListHeraBoardItems(c.ID, true)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, after, before)
}
