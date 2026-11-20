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

func TestRailBoardSessionAndPhoneLaunch(t *testing.T) {
	s, d, c, _, _ := boardRESTFixture(t)
	o, err := d.CreateHeraOrchestrator("other", "")
	testutil.NoError(t, err)
	other, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	_, err = d.PostHeraBoardItem(other.ID, "fyi", "Notice", "Body")
	testutil.NoError(t, err)
	handler := s.decisionMiddleware(authMiddleware("test-token", d, nil, s.routes(), "/api/hera/board/decision-session"))
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Argus-Auth", "master")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := call("POST", "/api/hera/board/decision-link", "{}", "test-token")
	testutil.Equal(t, w.Code, 200)
	var link struct{ Ticket, Path string }
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &link))
	testutil.Equal(t, link.Path, "/decide")
	w = call("POST", "/api/hera/board/decision-session", fmt.Sprintf(`{"ticket":%q}`, link.Ticket), "")
	testutil.Equal(t, w.Code, 200)
	var session struct {
		Session       string
		InitialRoleID int64 `json:"initial_role_id"`
	}
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &session))
	testutil.Equal(t, session.InitialRoleID > 0, true)
	for _, role := range []int64{c.ID, other.ID} {
		testutil.Equal(t, call("GET", fmt.Sprintf("/api/hera/roles/%d/board/decide-items", role), "", session.Session).Code, 200)
	}
	testutil.Equal(t, call("GET", "/api/hera/board/decision-navigation", "", session.Session).Code, 200)
	for _, path := range []string{"/api/hera", "/api/hera/board/decision-counts", fmt.Sprintf("/api/hera/roles/%d/board/fyi-actions/test", c.ID)} {
		testutil.Equal(t, call("GET", path, "", session.Session).Code, 403)
	}
	testutil.NoError(t, d.ArchiveHeraRole(other.ID))
	w = call("GET", fmt.Sprintf("/api/hera/roles/%d/board/decide-items", other.ID), "", session.Session)
	testutil.Equal(t, w.Code, 403)
	testutil.Contains(t, w.Body.String(), "board_not_visible")
	testutil.Equal(t, call("GET", fmt.Sprintf("/api/hera/roles/%d/board/decide-items", c.ID), "", session.Session).Code, 200)
	testutil.Equal(t, call("POST", "/api/hera/board/decision-link", "{} {}", "test-token").Code, 400)
}
