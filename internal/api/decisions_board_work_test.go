package api

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardWorkHTTPCountsAndCompletion(t *testing.T) {
	s, d, c, _, decision := boardRESTFixture(t)
	blocking, trigger := false, "after release"
	pending, err := d.PostHeraBoardItemWithMetadata(c.ID, "action", "later", "body", db.HeraBoardMetadataPatch{Blocking: &blocking, Trigger: &trigger})
	testutil.NoError(t, err)
	todo, err := d.PostHeraBoardItemWithMetadata(c.ID, "todo", "team work", "body", db.HeraBoardMetadataPatch{})
	testutil.NoError(t, err)
	w := decideCall(t, s, "GET", "/api/hera/board/decision-counts", "", "test-token")
	testutil.Equal(t, w.Code, 200)
	var counts struct{ Decisions, Actions, Todos, Total int }
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &counts))
	testutil.DeepEqual(t, counts, struct{ Decisions, Actions, Todos, Total int }{1, 1, 1, 1})
	session, _, err := s.decisions.mint(c.ID, true)
	testutil.NoError(t, err)
	base := fmt.Sprintf("/api/hera/roles/%d/board", c.ID)
	w = decideCall(t, s, "GET", base+"/decide-items", "", session)
	testutil.Equal(t, w.Code, 200)
	testutil.Contains(t, w.Body.String(), `"trigger_pending":true`)
	testutil.Contains(t, w.Body.String(), `"todos":1`)
	testutil.Contains(t, w.Body.String(), `"actions":1`)
	actions := []db.HeraBoardFYIAction{
		{ActionID: "human_step", ItemID: pending.ID, ExpectedUpdatedAt: pending.UpdatedAt, Action: "done"},
		{ActionID: "team_drop", ItemID: todo.ID, ExpectedUpdatedAt: todo.UpdatedAt, Action: "dropped", Note: "no longer needed"},
	}
	raw, err := json.Marshal(map[string]any{"actions": actions})
	testutil.NoError(t, err)
	for n := 0; n < 2; n++ {
		w = decideCall(t, s, "POST", base+"/fyi-actions", string(raw), session)
		testutil.Equal(t, w.Code, 200)
		var response struct{ Results []db.HeraBoardFYIResult }
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		testutil.Equal(t, len(response.Results), 2)
		testutil.Equal(t, response.Results[0].Outcome, "resolved")
		testutil.Equal(t, response.Results[1].Item.ResolutionChoice, "dropped")
	}
	testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 2)
	// Existing dismissal cannot bypass the new completion semantics.
	newAction, err := d.PostHeraBoardItemWithMetadata(c.ID, "action", "step", "body", db.HeraBoardMetadataPatch{Blocking: &blocking})
	testutil.NoError(t, err)
	dismiss := fmt.Sprintf(`{"expected_updated_at":%q}`, newAction.UpdatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))
	w = decideCall(t, s, "POST", fmt.Sprintf("%s/%d/dismiss", base, newAction.ID), dismiss, "test-token")
	testutil.Equal(t, w.Code, 400)
	open, err := d.ListHeraBoardItems(c.ID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(open), 2)
	testutil.Equal(t, open[0].ID, decision.ID)
	linkBody := fmt.Sprintf(`{"focus_item_id":%d}`, newAction.ID)
	testutil.Equal(t, decideCall(t, s, "POST", base+"/decision-link", linkBody, "test-token").Code, 200)
}
