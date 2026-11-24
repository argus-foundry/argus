package db

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func boardBool(v bool) *bool     { return &v }
func boardText(v string) *string { return &v }

func TestBoardWorkReadinessKeepsResponseRevision(t *testing.T) {
	for _, kind := range []string{"action", "todo"} {
		t.Run(kind, func(t *testing.T) {
			d := heraTestDB(t)
			c := mkRole(t, d, mkOrch(t, d, "revision").ID, "coord", HeraKindCoordinator)
			_, err := d.CreateHeraBinding(CreateHeraBindingInput{RoleID: c.ID, ArgusTaskID: "revision-task", WorktreePath: "/wt"})
			testutil.NoError(t, err)
			patch := HeraBoardMetadataPatch{Trigger: boardText("after release")}
			if kind == "action" {
				patch.Blocking = boardBool(false)
			}
			i, err := d.PostHeraBoardItemWithMetadata(c.ID, kind, "step", "body", patch)
			testutil.NoError(t, err)
			testutil.NoError(t, d.ReadHeraBoardItem(c.ID, i.ID))
			sink := sinkFor(t)
			ready, err := d.UpdateHeraBoardItemWithMetadata(c.ID, i.ID, kind, i.Subject, i.Body, HeraBoardMetadataPatch{TriggerPending: boardBool(false)})
			testutil.NoError(t, err)
			testutil.Equal(t, ready.UpdatedAt.Equal(i.UpdatedAt), true)
			testutil.Equal(t, ready.Read, false)
			testutil.Equal(t, ready.TriggerPending, false)
			evs := sink.board()
			testutil.Equal(t, len(evs), 1)
			testutil.Contains(t, string(evs[0].Payload), `"operation":"updated"`)
			// Ordering and read acknowledgments must not invalidate the frozen Done.
			_, err = d.ListHeraBoardItems(c.ID, false)
			testutil.NoError(t, err)
			a := HeraBoardFYIAction{ActionID: "done-before-ready", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "done"}
			results, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
			testutil.NoError(t, err)
			testutil.Equal(t, results[0].Outcome, "resolved")
			replay, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
			testutil.NoError(t, err)
			testutil.DeepEqual(t, replay, results)
			// A real authored edit still rejects a response to the earlier version.
			fresh, err := d.PostHeraBoardItemWithMetadata(c.ID, kind, "another step", "body", patch)
			testutil.NoError(t, err)
			edited, err := d.UpdateHeraBoardItem(c.ID, fresh.ID, kind, fresh.Subject, "a different step")
			testutil.NoError(t, err)
			testutil.Equal(t, edited.UpdatedAt.After(fresh.UpdatedAt), true)
			a = HeraBoardFYIAction{ActionID: "done-before-edit", ItemID: fresh.ID, ExpectedUpdatedAt: fresh.UpdatedAt, Action: "done"}
			results, err = d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
			testutil.NoError(t, err)
			testutil.Equal(t, results[0].Outcome, "conflict")
		})
	}
}

func TestBoardWorkMetadataAndAttention(t *testing.T) {
	d := heraTestDB(t)
	c := mkRole(t, d, mkOrch(t, d, "work").ID, "coord", HeraKindCoordinator)
	decision, err := d.PostHeraBoardItem(c.ID, "decision", "choose", "body")
	testutil.NoError(t, err)
	pending, err := d.PostHeraBoardItemWithMetadata(c.ID, "action", "later", "body", HeraBoardMetadataPatch{Blocking: boardBool(false), Trigger: boardText("after 1.42"), Command: boardText("run the backfill")})
	testutil.NoError(t, err)
	testutil.Equal(t, pending.TriggerPending, true)
	todo, err := d.PostHeraBoardItemWithMetadata(c.ID, "todo", "team work", "body", HeraBoardMetadataPatch{})
	testutil.NoError(t, err)
	blocking, err := d.PostHeraBoardItemWithMetadata(c.ID, "action", "unlock", "body", HeraBoardMetadataPatch{Blocking: boardBool(true), Command: boardText("Unlock 1Password")})
	testutil.NoError(t, err)
	items, err := d.ListHeraBoardItems(c.ID, false)
	testutil.NoError(t, err)
	testutil.DeepEqual(t, []int64{items[0].ID, items[1].ID, items[2].ID, items[3].ID}, []int64{blocking.ID, decision.ID, pending.ID, todo.ID})
	n, _, err := d.HeraBoardUnread(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, n, 2)
	testutil.NoError(t, d.ReadHeraBoardItem(c.ID, pending.ID))
	same, err := d.UpdateHeraBoardItemWithMetadata(c.ID, pending.ID, "action", pending.Subject, pending.Body, HeraBoardMetadataPatch{})
	testutil.NoError(t, err)
	testutil.Equal(t, same.Read, true)
	testutil.Equal(t, same.Command, pending.Command)
	ready, err := d.UpdateHeraBoardItemWithMetadata(c.ID, pending.ID, "action", pending.Subject, pending.Body, HeraBoardMetadataPatch{TriggerPending: boardBool(false), Blocking: boardBool(true)})
	testutil.NoError(t, err)
	testutil.Equal(t, ready.Read, false)
	testutil.Equal(t, ready.AttentionUnread(), true)
	cleared, err := d.UpdateHeraBoardItem(c.ID, pending.ID, "fyi", pending.Subject, pending.Body)
	testutil.NoError(t, err)
	testutil.Equal(t, cleared.Command, "")
	testutil.Equal(t, cleared.Trigger, "")
	testutil.Equal(t, cleared.Blocking, false)
}

func TestBoardWorkInvalidMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		patch      HeraBoardMetadataPatch
	}{
		{"missing blocking", "action", HeraBoardMetadataPatch{}},
		{"todo blocking", "todo", HeraBoardMetadataPatch{Blocking: boardBool(false)}},
		{"fyi trigger", "fyi", HeraBoardMetadataPatch{Trigger: boardText("later")}},
		{"pending blocker", "action", HeraBoardMetadataPatch{Blocking: boardBool(true), Trigger: boardText("later")}},
		{"no trigger", "todo", HeraBoardMetadataPatch{TriggerPending: boardBool(true)}},
		{"blank command", "action", HeraBoardMetadataPatch{Blocking: boardBool(false), Command: boardText("  ")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := heraTestDB(t)
			c := mkRole(t, d, mkOrch(t, d, "work").ID, "coord", HeraKindCoordinator)
			_, err := d.PostHeraBoardItemWithMetadata(c.ID, tc.kind, "step", "body", tc.patch)
			testutil.ErrorIs(t, err, ErrHeraBoardInvalid)
		})
	}
}

func TestBoardWorkCompletion(t *testing.T) {
	for _, kind := range []string{"action", "todo"} {
		for _, outcome := range []string{"done", "dropped"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				d := heraTestDB(t)
				c := mkRole(t, d, mkOrch(t, d, "work").ID, "coord", HeraKindCoordinator)
				p := HeraBoardMetadataPatch{}
				if kind == "action" {
					p.Blocking = boardBool(true)
				}
				i, err := d.PostHeraBoardItemWithMetadata(c.ID, kind, "step", "body", p)
				testutil.NoError(t, err)
				a := HeraBoardFYIAction{ActionID: "complete", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: outcome, Note: "optional"}
				results, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
				testutil.NoError(t, err)
				if kind == "action" && outcome == "dropped" {
					testutil.Equal(t, results[0].Outcome, "invalid")
					return
				}
				testutil.Equal(t, results[0].Outcome, "resolved")
				testutil.Equal(t, results[0].Item.ResolutionChoice, outcome)
				after, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
				testutil.NoError(t, err)
				testutil.DeepEqual(t, after, results)
				messages, err := d.HeraInbox(c.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, len(messages), 1)
				testutil.Contains(t, messages[0].Body, outcome)
			})
		}
	}
}

func TestBoardWorkCompletionConflictAndRollback(t *testing.T) {
	d := heraTestDB(t)
	c := mkRole(t, d, mkOrch(t, d, "work").ID, "coord", HeraKindCoordinator)
	i, err := d.PostHeraBoardItemWithMetadata(c.ID, "action", "step", "body", HeraBoardMetadataPatch{Blocking: boardBool(true)})
	testutil.NoError(t, err)
	a := HeraBoardFYIAction{ActionID: "complete", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt.Add(-time.Second), Action: "done"}
	results, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
	testutil.NoError(t, err)
	testutil.Equal(t, results[0].Outcome, "conflict")
	a.ExpectedUpdatedAt = i.UpdatedAt
	_, err = d.conn.Exec(`CREATE TRIGGER reject_work_notice BEFORE INSERT ON hera_messages BEGIN SELECT RAISE(ABORT, 'test'); END`)
	testutil.NoError(t, err)
	_, err = d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
	testutil.Error(t, err)
	items, err := d.ListHeraBoardItems(c.ID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	_, err = d.HeraBoardFYIReceipt(c.ID, a.ActionID)
	testutil.ErrorIs(t, err, ErrHeraBoardNotFound)
}

func TestBoardWorkTerminalOutcomeAndReceipts(t *testing.T) {
	d := heraTestDB(t)
	c := mkRole(t, d, mkOrch(t, d, "work").ID, "coord", HeraKindCoordinator)
	i, err := d.PostHeraBoardItem(c.ID, "todo", "team work", "body")
	testutil.NoError(t, err)
	a := HeraBoardFYIAction{ActionID: "first", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "done"}
	result, err := d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
	testutil.NoError(t, err)
	testutil.Equal(t, result[0].Outcome, "resolved")
	a.Action = "dropped"
	for _, id := range []string{"first", "different"} {
		t.Run(id, func(t *testing.T) {
			a.ActionID = id
			result, err = d.FYIHeraBoardItems(c.ID, []HeraBoardFYIAction{a})
			testutil.NoError(t, err)
			testutil.Equal(t, result[0].Outcome, "invalid")
		})
	}
	testutil.ErrorIs(t, d.ResolveHeraBoardWorkItem(c.ID, i.ID, "dropped", ""), ErrHeraBoardResolved)
	testutil.NoError(t, d.ResolveHeraBoardWorkItem(c.ID, i.ID, "done", ""))
	messages, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(messages), 1)
}

func TestBoardWorkValidationLimitsAndClearing(t *testing.T) {
	d := heraTestDB(t)
	c := mkRole(t, d, mkOrch(t, d, "work").ID, "coord", HeraKindCoordinator)
	for _, tc := range []struct {
		name string
		p    HeraBoardMetadataPatch
	}{
		{"long trigger", HeraBoardMetadataPatch{Blocking: boardBool(false), Trigger: boardText(strings.Repeat("é", 513))}},
		{"long command", HeraBoardMetadataPatch{Blocking: boardBool(false), Command: boardText(strings.Repeat("é", 8193))}},
		{"invalid trigger utf8", HeraBoardMetadataPatch{Blocking: boardBool(false), Trigger: boardText("\xff")}},
		{"invalid command utf8", HeraBoardMetadataPatch{Blocking: boardBool(false), Command: boardText("\xff")}},
		{"blank trigger", HeraBoardMetadataPatch{Blocking: boardBool(false), Trigger: boardText("  ")}},
		{"todo command", HeraBoardMetadataPatch{Command: boardText("step")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := "action"
			if tc.name == "todo command" {
				kind = "todo"
			}
			_, err := d.PostHeraBoardItemWithMetadata(c.ID, kind, "step", "body", tc.p)
			testutil.ErrorIs(t, err, ErrHeraBoardInvalid)
		})
	}
	i, err := d.PostHeraBoardItemWithMetadata(c.ID, "action", "step", "body", HeraBoardMetadataPatch{Blocking: boardBool(false), Trigger: boardText("after release"), Command: boardText("step")})
	testutil.NoError(t, err)
	i, err = d.UpdateHeraBoardItemWithMetadata(c.ID, i.ID, i.Kind, i.Subject, i.Body, HeraBoardMetadataPatch{Trigger: boardText(""), TriggerPending: boardBool(false), Command: boardText("")})
	testutil.NoError(t, err)
	testutil.Equal(t, i.Trigger, "")
	testutil.Equal(t, i.Command, "")
	testutil.Equal(t, i.TriggerPending, false)
}

func TestBoardWorkNotMisclassifiedInDecisionSummary(t *testing.T) {
	d := heraTestDB(t)
	c := mkRole(t, d, mkOrch(t, d, "work").ID, "coord", HeraKindCoordinator)
	decision, err := d.PostHeraBoardItem(c.ID, "decision", "choose", "body", decisionFixture())
	testutil.NoError(t, err)
	todo, err := d.PostHeraBoardItem(c.ID, "todo", "future", "body")
	testutil.NoError(t, err)
	fyi, err := d.PostHeraBoardItem(c.ID, "fyi", "note", "body")
	testutil.NoError(t, err)
	_, err = d.AnswerHeraBoardItems(c.ID, []HeraBoardAnswer{{ItemID: decision.ID, ExpectedUpdatedAt: decision.UpdatedAt, Choice: "retry"}})
	testutil.NoError(t, err)
	messages, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(messages), 1)
	testutil.Contains(t, messages[0].Body, fmt.Sprintf("Still open FYIs: #%d", fyi.ID))
	testutil.Equal(t, strings.Contains(messages[0].Body, fmt.Sprintf("#%d", todo.ID)), false)
}
