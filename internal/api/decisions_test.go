package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/uxlog"
)

func TestDecisionCredentials(t *testing.T) {
	var store decisionStore
	now := time.Now()
	store.now = func() time.Time { return now }
	ticket, _, err := store.mint(12, false)
	testutil.NoError(t, err)
	role, ok := store.lookup(ticket, true)
	testutil.Equal(t, ok, true)
	testutil.Equal(t, role, int64(12))
	_, ok = store.lookup(ticket, true)
	testutil.Equal(t, ok, false)
	ticket, _, err = store.mint(12, false)
	testutil.NoError(t, err)
	now = now.Add(90 * time.Second)
	_, ok = store.lookup(ticket, true)
	testutil.Equal(t, ok, false)
	session, _, err := store.mint(12, true)
	testutil.NoError(t, err)
	_, ok = store.lookup(session, false)
	testutil.Equal(t, ok, true)
	now = now.Add(8 * time.Hour)
	_, ok = store.lookup(session, false)
	testutil.Equal(t, ok, false)
	var restarted decisionStore
	_, ok = restarted.lookup(session, false)
	testutil.Equal(t, ok, false)
	store.entries = make([]decisionCredential, 4096)
	for n := range store.entries {
		store.entries[n].expires = now.Add(time.Hour)
	}
	_, _, err = store.mint(12, false)
	testutil.Error(t, err)
	now = now.Add(time.Hour)
	_, _, err = store.mint(12, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(store.entries), 1)
}

func TestDecisionSessionRouteDenylist(t *testing.T) {
	s, d, c, _, _ := boardRESTFixture(t)
	session, _, err := s.decisions.mint(c.ID, true)
	testutil.NoError(t, err)
	handler := s.decisionMiddleware(authMiddleware("test-token", d, nil, s.routes(), "/decide", "/api/hera/board/decision-session"))
	source, err := os.ReadFile("routes.go")
	testutil.NoError(t, err)
	patterns := regexp.MustCompile(`mux.HandleFunc\("([A-Z]+) ([^"]+)"`).FindAllStringSubmatch(string(source), -1)
	testutil.Equal(t, len(patterns) > 100, true)
	for _, pattern := range patterns {
		t.Run(pattern[1]+" "+pattern[2], func(t *testing.T) {
			path := regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(pattern[2], fmt.Sprint(c.ID))
			if strings.HasSuffix(path, "/board/decisions") || strings.HasSuffix(path, "/board/answers") {
				return
			}
			r := httptest.NewRequest(pattern[1], path, strings.NewReader("{}"))
			r.Header.Set("Authorization", "Bearer "+session)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			testutil.Equal(t, w.Code, 403)
		})
	}
}

func TestDecisionRequestValidation(t *testing.T) {
	s, d, c, _, i := boardRESTFixture(t)
	device, _, err := MintToken(d, "phone")
	testutil.NoError(t, err)
	plugin, _, err := MintTokenWithScope(d, "plugin", "sample")
	testutil.NoError(t, err)
	worker, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: c.OrchestratorID, Name: "worker", Kind: db.HeraKindWorker})
	testutil.NoError(t, err)
	handler := s.decisionMiddleware(authMiddleware("test-token", d, nil, s.routes(), "/decide", "/api/hera/board/decision-session"))
	base := fmt.Sprintf("/api/hera/roles/%d/board", c.ID)
	for _, tc := range []struct {
		name, method, path, body, token string
		status                          int
	}{
		{"device reads", "GET", base + "/decisions", "", device, 200},
		{"master reads", "GET", base + "/decisions", "", "test-token", 200},
		{"plugin cannot read", "GET", base + "/decisions", "", plugin, 403},
		{"plugin cannot mint", "POST", base + "/decision-link", "{}", plugin, 403},
		{"device mints optional body", "POST", base + "/decision-link", "", device, 200},
		{"focus", "POST", base + "/decision-link", fmt.Sprintf(`{"focus_item_id":%d}`, i.ID), device, 200},
		{"invalid focus", "POST", base + "/decision-link", `{"focus_item_id":99999}`, device, 400},
		{"invalid role id", "GET", "/api/hera/roles/no/board/decisions", "", device, 400},
		{"missing role", "POST", "/api/hera/roles/99999/board/decision-link", "{}", device, 404},
		{"worker", "POST", fmt.Sprintf("/api/hera/roles/%d/board/decision-link", worker.ID), "{}", device, 400},
		{"unknown ticket", "POST", "/api/hera/board/decision-session", `{"ticket":"unknown"}`, "", 401},
		{"bad exchange", "POST", "/api/hera/board/decision-session", `{} {}`, "", 400},
		{"trailing mint json", "POST", base + "/decision-link", `{} {}`, device, 400},
		{"zero revision", "POST", base + "/answers", fmt.Sprintf(`{"answers":[{"item_id":%d,"expected_updated_at":"0001-01-01T00:00:00Z","choice":"other","note":"answer"}]}`, i.ID), device, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			testutil.Equal(t, w.Code, tc.status)
		})
	}
	testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "agent", ""))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", base+"/decision-link", strings.NewReader("{}"))
	r.Header.Set("Authorization", "Bearer "+device)
	handler.ServeHTTP(w, r)
	testutil.Equal(t, w.Code, 409)
}

func TestDecisionMixedAnswers(t *testing.T) {
	s, d, c, _, one := boardRESTFixture(t)
	two, err := d.PostHeraBoardItem(c.ID, "decision", "Two", "Old")
	testutil.NoError(t, err)
	three, err := d.PostHeraBoardItem(c.ID, "decision", "Three", "Body")
	testutil.NoError(t, err)
	_, err = d.UpdateHeraBoardItem(c.ID, two.ID, "decision", "Two", "Changed")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, three.ID, "agent", ""))
	body, err := json.Marshal(map[string]any{"answers": []db.HeraBoardAnswer{{ItemID: one.ID, ExpectedUpdatedAt: one.UpdatedAt, Choice: "other", Note: "Answer"}, {ItemID: two.ID, ExpectedUpdatedAt: two.UpdatedAt, Choice: "other", Note: "Draft"}, {ItemID: three.ID, ExpectedUpdatedAt: three.UpdatedAt, Choice: "other", Note: "Too late"}}})
	testutil.NoError(t, err)
	handler := s.decisionMiddleware(authMiddleware("test-token", d, nil, s.routes()))
	call := func() *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", fmt.Sprintf("/api/hera/roles/%d/board/answers", c.ID), strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer test-token")
		handler.ServeHTTP(w, r)
		return w
	}
	w := call()
	testutil.Equal(t, w.Code, 200)
	var result struct{ Results []db.HeraBoardAnswerResult }
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	testutil.Equal(t, result.Results[0].Outcome, "resolved")
	testutil.Equal(t, result.Results[1].Outcome, "conflict")
	testutil.Equal(t, result.Results[1].Item.Body, "Changed")
	testutil.Equal(t, result.Results[2].Outcome, "already_resolved")
	testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 1)
	testutil.Equal(t, call().Code, 200)
	testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 1)
}

func TestDecisionCredentialExpiryThroughHTTP(t *testing.T) {
	s, d, c, _, _ := boardRESTFixture(t)
	now := time.Now()
	s.decisions.now = func() time.Time { return now }
	session, _, err := s.decisions.mint(c.ID, true)
	testutil.NoError(t, err)
	handler := s.decisionMiddleware(authMiddleware("test-token", d, nil, s.routes()))
	for _, tc := range []struct {
		name, header string
		advance      time.Duration
	}{{"non bearer", session, 0}, {"expired", "Bearer " + session, 8 * time.Hour}} {
		t.Run(tc.name, func(t *testing.T) {
			now = now.Add(tc.advance)
			r := httptest.NewRequest("GET", fmt.Sprintf("/api/hera/roles/%d/board/decisions", c.ID), nil)
			r.Header.Set("Authorization", tc.header)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			testutil.Equal(t, w.Code, 401)
		})
	}
}

func TestDecisionHTTP(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ux.log")
	testutil.NoError(t, uxlog.Init(logPath))
	t.Cleanup(uxlog.Close)
	s, d, c, other, item := boardRESTFixture(t)
	handler := s.decisionMiddleware(authMiddleware("test-token", d, nil, s.routes(), "/decide", "/api/hera/board/decision-session"))
	req := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	base := fmt.Sprintf("/api/hera/roles/%d/board", c.ID)
	w := req("POST", base+"/decision-link", "{}", "test-token")
	testutil.Equal(t, w.Code, 200)
	var link struct{ Ticket, Path string }
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &link))
	testutil.Contains(t, link.Path, "/decide?role=")
	w = req("POST", "/api/hera/board/decision-session", fmt.Sprintf(`{"ticket":%q}`, link.Ticket), "")
	testutil.Equal(t, w.Code, 200)
	var session struct{ Session string }
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &session))
	defer func() {
		data, err := os.ReadFile(logPath)
		testutil.NoError(t, err)
		testutil.Contains(t, string(data), "[decide] minted")
		testutil.Contains(t, string(data), "outcome=resolved")
		testutil.Equal(t, strings.Contains(string(data), link.Ticket), false)
		testutil.Equal(t, strings.Contains(string(data), session.Session), false)
		testutil.Equal(t, strings.Contains(string(data), "Use a custom answer"), false)
	}()
	testutil.Equal(t, req("POST", "/api/hera/board/decision-session", fmt.Sprintf(`{"ticket":%q}`, link.Ticket), "").Code, 401)
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", base + "/decisions", 200}, {"GET", base, 403}, {"GET", fmt.Sprintf("/api/hera/roles/%d/board/decisions", other.ID), 403}, {"POST", base + "/decision-link", 403}, {"GET", "/api/tasks", 403}, {"POST", "/api/mcp/tools", 403}, {"GET", "/api/plugin-views", 403}} {
		t.Run(tc.method+tc.path, func(t *testing.T) { testutil.Equal(t, req(tc.method, tc.path, "{}", session.Session).Code, tc.status) })
	}
	for _, body := range []string{`{}`, `{"answers":[]}`, `{"answers":[{"item_id":1}]}`, `{} {}`, "{}" + strings.Repeat(" ", 500000)} {
		testutil.Equal(t, req("POST", base+"/answers", body, session.Session).Code, 400)
	}
	body := fmt.Sprintf(`{"answers":[{"item_id":%d,"expected_updated_at":%q,"choice":"other","note":"Use a custom answer"}]}`, item.ID, ts(item.UpdatedAt))
	w = req("POST", base+"/answers", body, session.Session)
	testutil.Equal(t, w.Code, 200)
	testutil.Contains(t, w.Body.String(), `"outcome":"resolved"`)
	testutil.Equal(t, len(mustMsgs(t, d, c.ID)), 1)
	w = req("GET", "/decide", "", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Equal(t, strings.Contains(w.Body.String(), "Use a custom answer"), false)
	w = req("GET", base+"?include_resolved=true", "", "test-token")
	testutil.Equal(t, w.Code, 200)
	testutil.Contains(t, w.Body.String(), `"structured":null`)
	testutil.Contains(t, w.Body.String(), `"resolution_choice":"other"`)
}

func TestDecisionStructuredJSON(t *testing.T) {
	s, d, c, _, item := boardRESTFixture(t)
	structure := &db.HeraBoardStructure{Options: []db.HeraBoardOption{{ID: "retry", Label: "Retry"}, {ID: "wait", Label: "Wait"}}, Recommended: "retry", Reason: "Recover"}
	item, err := d.UpdateHeraBoardItem(c.ID, item.ID, item.Kind, item.Subject, item.Body, structure)
	testutil.NoError(t, err)
	_, err = d.PostHeraBoardItem(c.ID, "fyi", "Only a notice", "Body")
	testutil.NoError(t, err)
	handler := s.decisionMiddleware(authMiddleware("test-token", d, nil, s.routes()))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", fmt.Sprintf("/api/hera/roles/%d/board/decisions", c.ID), nil)
	r.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(w, r)
	testutil.Equal(t, w.Code, 200)
	var response struct {
		RoleID      int64               `json:"role_id"`
		Coordinator string              `json:"coordinator"`
		Items       []*db.HeraBoardItem `json:"items"`
	}
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	testutil.Equal(t, response.RoleID, c.ID)
	testutil.Equal(t, response.Coordinator, c.Name)
	testutil.Equal(t, len(response.Items), 1)
	testutil.DeepEqual(t, response.Items[0].Structured, structure)
	testutil.Equal(t, response.Items[0].ResolutionChoice, "")
	testutil.Equal(t, response.Items[0].UpdatedAt, item.UpdatedAt)
	count, _, err := d.HeraBoardUnread(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, count, 2)
	testutil.Equal(t, response.Items[0].Read, false)
}
