package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func decideCall(t *testing.T, s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.decisionMiddleware(authMiddleware("test-token", s.db, nil, s.routes(), "/api/hera/board/decision-session")).ServeHTTP(w, r)
	return w
}

func TestFYIHTTPActionsAndReceipts(t *testing.T) {
	s, d, c, _, decision := boardRESTFixture(t)
	i, err := d.PostHeraBoardItem(c.ID, "fyi", "Notice", "Body")
	testutil.NoError(t, err)
	session, _, err := s.decisions.mint(c.ID, true)
	testutil.NoError(t, err)
	base := fmt.Sprintf("/api/hera/roles/%d/board", c.ID)
	action := db.HeraBoardFYIAction{ActionID: "comment_1", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Action: "comment", Note: "Full comment"}
	encode := func(actions []db.HeraBoardFYIAction) string {
		raw, e := json.Marshal(map[string]any{"actions": actions})
		testutil.NoError(t, e)
		return string(raw)
	}
	call := func(body string) *httptest.ResponseRecorder {
		return decideCall(t, s, "POST", base+"/fyi-actions", body, session)
	}
	for _, tc := range []struct {
		name    string
		actions []db.HeraBoardFYIAction
	}{
		{"empty", nil}, {"duplicate", []db.HeraBoardFYIAction{action, action}},
		{"invalid id", []db.HeraBoardFYIAction{{ActionID: "bad id", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt}}},
		{"oversized", []db.HeraBoardFYIAction{{ActionID: "big", ItemID: i.ID, ExpectedUpdatedAt: i.UpdatedAt, Note: strings.Repeat("x", 4097)}}},
		{"zero revision", []db.HeraBoardFYIAction{{ActionID: "zero", ItemID: i.ID}}},
	} {
		t.Run(tc.name, func(t *testing.T) { testutil.Equal(t, call(encode(tc.actions)).Code, 400) })
	}
	testutil.Equal(t, call(encode([]db.HeraBoardFYIAction{action})+" {}").Code, 400)
	testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 0)
	body := encode([]db.HeraBoardFYIAction{action})
	for n := 0; n < 2; n++ {
		w := call(body)
		testutil.Equal(t, w.Code, 200)
		testutil.Contains(t, w.Body.String(), `"outcome":"commented"`)
		testutil.Equal(t, w.Header().Get("Cache-Control"), "no-store")
	}
	testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 1)
	w := decideCall(t, s, "GET", base+"/fyi-actions/comment_1", "", "test-token")
	testutil.Equal(t, w.Code, 200)
	testutil.Contains(t, w.Body.String(), "Full comment")
	testutil.Equal(t, decideCall(t, s, "GET", base+"/fyi-actions/comment_1", "", session).Code, 403)
	plugin, _, err := MintTokenWithScope(d, "plugin", "sample")
	testutil.NoError(t, err)
	testutil.Equal(t, decideCall(t, s, "GET", base+"/fyi-actions/comment_1", "", plugin).Code, 403)
	testutil.Equal(t, decideCall(t, s, "GET", base+"/fyi-actions/unknown", "", "test-token").Code, 404)
	action.Note = "different"
	testutil.Contains(t, call(encode([]db.HeraBoardFYIAction{action})).Body.String(), `"outcome":"invalid"`)
	action.ActionID = "wrong_kind"
	action.ItemID = decision.ID
	testutil.Contains(t, call(encode([]db.HeraBoardFYIAction{action})).Body.String(), `"outcome":"not_found"`)
	testutil.NoError(t, d.ArchiveHeraRole(c.ID))
	testutil.Equal(t, decideCall(t, s, "GET", base+"/fyi-actions/comment_1", "", "test-token").Code, 200)
}

func TestDecisionCountsAndEmptyLaunch(t *testing.T) {
	s, d, c, _, i := boardRESTFixture(t)
	f, err := d.PostHeraBoardItem(c.ID, "fyi", "Notice", "Body")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ReadHeraBoardItem(c.ID, f.ID))
	w := decideCall(t, s, "GET", "/api/hera/board/decision-counts", "", "test-token")
	testutil.Equal(t, w.Code, 200)
	var counts struct{ Decisions, FYIs, Total int }
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &counts))
	testutil.DeepEqual(t, counts, struct{ Decisions, FYIs, Total int }{1, 1, 2})
	testutil.Equal(t, w.Header().Get("Cache-Control"), "no-store")
	for _, item := range []*db.HeraBoardItem{i, f} {
		testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, item.ID, "agent", ""))
	}
	w = decideCall(t, s, "POST", fmt.Sprintf("/api/hera/roles/%d/board/decision-link", c.ID), "{}", "test-token")
	testutil.Equal(t, w.Code, 200)
	for _, body := range []string{"null", "[]", `{"role_id":9}`, "{} {}"} {
		testutil.Equal(t, decideCall(t, s, "POST", "/api/hera/board/decision-link", body, "test-token").Code, 400)
	}
	testutil.NoError(t, d.ArchiveHeraRole(c.ID))
	// Remove the fixture's second coordinator too, leaving a permitted empty roster.
	roles, err := d.ListHeraRoles(c.OrchestratorID, false)
	testutil.NoError(t, err)
	for _, r := range roles {
		testutil.NoError(t, d.ArchiveHeraRole(r.ID))
	}
	w = decideCall(t, s, "POST", "/api/hera/board/decision-link", "", "test-token")
	testutil.Equal(t, w.Code, 200)
	var link struct{ Ticket string }
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &link))
	w = decideCall(t, s, "POST", "/api/hera/board/decision-session", fmt.Sprintf(`{"ticket":%q}`, link.Ticket), "")
	testutil.Equal(t, w.Code, 200)
	testutil.Contains(t, w.Body.String(), `"initial_role_id":null`)
}

func TestRailBoardNestedVisibilityOnEveryWrite(t *testing.T) {
	s, d, c, _, _ := boardRESTFixture(t)
	child, err := d.CreateHeraOrchestrator("child", "")
	testutil.NoError(t, err)
	role, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: child.ID, Name: "coord", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	testutil.NoError(t, d.CreateHeraOrchLink(c.OrchestratorID, child.ID, c.ID))
	f, err := d.PostHeraBoardItem(role.ID, "fyi", "Notice", "Body")
	testutil.NoError(t, err)
	session, _, err := s.decisions.mint(c.ID, true)
	testutil.NoError(t, err)
	w := decideCall(t, s, "GET", "/api/hera/board/decision-navigation", "", session)
	testutil.Equal(t, w.Code, 200)
	testutil.Contains(t, w.Body.String(), fmt.Sprintf(`"parent_role_id":%d`, c.ID))
	body := fmt.Sprintf(`{"actions":[{"action_id":"hide","item_id":%d,"expected_updated_at":%q,"action":"dismiss","note":""}]}`, f.ID, ts(f.UpdatedAt))
	path := fmt.Sprintf("/api/hera/roles/%d/board/fyi-actions", role.ID)
	testutil.NoError(t, d.ArchiveHeraRole(c.ID))
	w = decideCall(t, s, "POST", path, body, session)
	testutil.Equal(t, w.Code, http.StatusForbidden)
	testutil.Contains(t, w.Body.String(), "board_not_visible")
	items, err := d.ListHeraBoardItems(role.ID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
}

func TestClosedBoardFeedbackHTTPContext(t *testing.T) {
	for _, kind := range []string{"fyi", "decision"} {
		t.Run(kind, func(t *testing.T) {
			s, d, c, _, _ := boardRESTFixture(t)
			closed, err := d.PostHeraBoardItem(c.ID, kind, "Closed during review", "body")
			testutil.NoError(t, err)
			fresh, err := d.PostHeraBoardItem(c.ID, kind, "Still open", "body")
			testutil.NoError(t, err)
			testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, closed.ID, "agent", "done"))
			session, _, err := s.decisions.mint(c.ID, true)
			testutil.NoError(t, err)
			field, endpoint := "actions", "fyi-actions"
			if kind == "decision" {
				field, endpoint = "answers", "answers"
			}
			body := fmt.Sprintf(`{%q:[{"action_id":"closed","item_id":%d,"expected_updated_at":%q,"action":"comment","note":"Keep this"},{"action_id":"fresh","item_id":%d,"expected_updated_at":%q,"action":"comment","note":"Accept this"}]}`, field, closed.ID, ts(closed.UpdatedAt), fresh.ID, ts(fresh.UpdatedAt))
			w := decideCall(t, s, "POST", fmt.Sprintf("/api/hera/roles/%d/board/%s", c.ID, endpoint), body, session)
			testutil.Equal(t, w.Code, 200)
			var response struct{ Results []db.HeraBoardFYIResult }
			testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			testutil.Equal(t, len(response.Results), 2)
			testutil.Equal(t, response.Results[0].Outcome, "invalid")
			testutil.Equal(t, response.Results[0].ItemID, closed.ID)
			testutil.NotNil(t, response.Results[0].Item)
			if response.Results[0].Item != nil {
				testutil.Equal(t, response.Results[0].Item.Resolved, true)
			}
			testutil.Equal(t, response.Results[1].Outcome, "commented")
			testutil.Equal(t, response.Results[1].ItemID, fresh.ID)
			testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 1)
		})
	}
}
