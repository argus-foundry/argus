package db

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func boardBool(v bool) *bool     { return &v }
func boardText(v string) *string { return &v }

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
