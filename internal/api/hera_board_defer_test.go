package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardDeferREST(t *testing.T) {
	s, d, c, c2, i := boardRESTFixture(t)
	path := fmt.Sprintf("/api/hera/roles/%d/board/%d/defer", c.ID, i.ID)
	body := fmt.Sprintf(`{"deferred":true,"expected_updated_at":%q}`, ts(i.UpdatedAt))
	w := doBoard(s, "PUT", path, body)
	testutil.Equal(t, w.Code, 200)
	var got struct {
		Item   db.HeraBoardItem `json:"item"`
		Unread int              `json:"unread_count"`
	}
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	testutil.Equal(t, got.Item.DeferredAt.IsZero(), false)
	testutil.Equal(t, got.Unread, 0)
	w = doBoard(s, "GET", fmt.Sprintf("/api/hera/roles/%d/board", c.ID), "")
	testutil.Contains(t, w.Body.String(), `"unread_count":0`)
	testutil.Contains(t, w.Body.String(), `"deferred_count":1`)
	testutil.Equal(t, doBoard(s, "PUT", fmt.Sprintf("/api/hera/roles/%d/board/%d/defer", c2.ID, i.ID), body).Code, 404)
	for _, bad := range []string{`{}`, `{"deferred":true}`, body + ` {}`, body + " garbage"} {
		t.Run(bad, func(t *testing.T) { testutil.Equal(t, doBoard(s, "PUT", path, bad).Code, 400) })
	}
	_, err := d.UpdateHeraBoardItem(c.ID, i.ID, i.Kind, i.Subject, "new content")
	testutil.NoError(t, err)
	testutil.Equal(t, doBoard(s, "PUT", path, body).Code, 409)
}

func TestBoardDeferSessionAndDefaultList(t *testing.T) {
	s, d, c, _, i := boardRESTFixture(t)
	session, _, err := s.decisions.mint(c.ID, true)
	testutil.NoError(t, err)
	handler := s.decisionMiddleware(authMiddleware("test-token", d, nil, s.routes()))
	path := fmt.Sprintf("/api/hera/roles/%d/board/%d/defer", c.ID, i.ID)
	body := fmt.Sprintf(`{"deferred":true,"expected_updated_at":%q}`, ts(i.UpdatedAt))
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+session)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	testutil.Equal(t, call("PUT", path, body).Code, 200)
	base := fmt.Sprintf("/api/hera/roles/%d/board", c.ID)
	plugin, _, err := MintTokenWithScope(d, "plugin", "fixture")
	testutil.NoError(t, err)
	req := httptest.NewRequest("PUT", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+plugin)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, req)
	testutil.Equal(t, denied.Code, 403)
	w := call("GET", base+"/decide-items", "")
	testutil.Equal(t, w.Code, 200)
	testutil.Contains(t, w.Body.String(), `"items":[]`)
	testutil.Contains(t, w.Body.String(), `"deferred_count":1`)
	w = call("GET", base+"/decide-items?include_deferred=true", "")
	testutil.Contains(t, w.Body.String(), `"subject":"Pick"`)
	testutil.NoError(t, d.ArchiveHeraRole(c.ID))
	testutil.Equal(t, call("PUT", path, body).Code, 403)
}
