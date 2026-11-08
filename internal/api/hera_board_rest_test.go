package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type boardSink struct {
	mu  sync.Mutex
	evs []model.Event
}

func (b *boardSink) Emit(ev model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ev.Type == model.EventTypeHeraBoardChanged {
		b.evs = append(b.evs, ev)
	}
}

func (b *boardSink) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.evs)
}

func boardRESTFixture(t *testing.T) (*Server, *db.DB, *db.HeraRole, *db.HeraRole, *db.HeraBoardItem) {
	t.Helper()
	s, d := testServer(t)
	o, err := d.CreateHeraOrchestrator("o", "")
	testutil.NoError(t, err)
	c, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	c2, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: o.ID, Name: "coord2", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	i, err := d.PostHeraBoardItem(c.ID, "decision", "Pick", "body")
	testutil.NoError(t, err)
	return s, d, c, c2, i
}

func doBoard(s *Server, method, path, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	s.routes().ServeHTTP(rr, authedReq(method, path, body))
	return rr
}

func ts(t time.Time) string { return t.Format(time.RFC3339Nano) }

func codeOf(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct{ Code string }
	testutil.NoError(t, json.Unmarshal(rr.Body.Bytes(), &e))
	return e.Code
}

func TestBoardReadState(t *testing.T) {
	s, d, c, c2, i := boardRESTFixture(t)
	sink := &boardSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	base := fmt.Sprintf("/api/hera/roles/%d/board/%d/read-state", c.ID, i.ID)

	rr := doBoard(s, "PUT", base, fmt.Sprintf(`{"read":true,"expected_updated_at":%q}`, ts(i.UpdatedAt)))
	testutil.Equal(t, rr.Code, 200)
	var got struct {
		Item   db.HeraBoardItem `json:"item"`
		Unread int              `json:"unread_count"`
	}
	testutil.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	testutil.Equal(t, got.Item.Read, true)
	testutil.Equal(t, got.Unread, 0)
	testutil.Equal(t, getHera(t, s).Orchestrators[0].Roles[0].BoardUnread, 0)
	testutil.Equal(t, sink.count(), 1)

	rr = doBoard(s, "PUT", base, fmt.Sprintf(`{"read":false,"expected_updated_at":%q}`, ts(i.UpdatedAt)))
	testutil.Equal(t, rr.Code, 200)
	testutil.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	testutil.Equal(t, got.Unread, 1)
	testutil.Equal(t, getHera(t, s).Orchestrators[0].Roles[0].BoardUnread, 1)

	t.Run("GET does not change read state", func(t *testing.T) {
		before := sink.count()
		testutil.Equal(t, doBoard(s, "GET", fmt.Sprintf("/api/hera/roles/%d/board", c.ID), "").Code, 200)
		n, _, _ := d.HeraBoardUnread(c.ID)
		testutil.Equal(t, n, 1)
		testutil.Equal(t, sink.count(), before)
	})
	t.Run("malformed", func(t *testing.T) {
		for _, tc := range []struct{ name, path, body string }{
			{"bad json", base, `{`},
			{"missing read", base, fmt.Sprintf(`{"expected_updated_at":%q}`, ts(i.UpdatedAt))},
			{"bad ts", base, `{"read":true,"expected_updated_at":"nope"}`},
			{"missing ts", base, `{"read":true}`},
			{"bad item id", fmt.Sprintf("/api/hera/roles/%d/board/x/read-state", c.ID), `{}`},
		} {
			rr := doBoard(s, "PUT", tc.path, tc.body)
			testutil.Equal(t, rr.Code, 400)
			testutil.Equal(t, codeOf(t, rr), "invalid_request")
		}
	})
	t.Run("ownership mismatch and missing", func(t *testing.T) {
		for _, p := range []string{
			fmt.Sprintf("/api/hera/roles/%d/board/%d/read-state", c2.ID, i.ID),
			fmt.Sprintf("/api/hera/roles/%d/board/9999/read-state", c.ID),
			fmt.Sprintf("/api/hera/roles/9999/board/%d/read-state", i.ID),
		} {
			rr := doBoard(s, "PUT", p, fmt.Sprintf(`{"read":true,"expected_updated_at":%q}`, ts(i.UpdatedAt)))
			testutil.Equal(t, rr.Code, 404)
			testutil.Equal(t, codeOf(t, rr), "board_item_not_found")
		}
		n, _, _ := d.HeraBoardUnread(c.ID)
		testutil.Equal(t, n, 1)
	})
	t.Run("stale revision leaves changed item unread", func(t *testing.T) {
		_, err := d.UpdateHeraBoardItem(c.ID, i.ID, "decision", "Pick", "edited")
		testutil.NoError(t, err)
		rr := doBoard(s, "PUT", base, fmt.Sprintf(`{"read":true,"expected_updated_at":%q}`, ts(i.UpdatedAt)))
		testutil.Equal(t, rr.Code, 409)
		testutil.Equal(t, codeOf(t, rr), "revision_conflict")
		n, _, _ := d.HeraBoardUnread(c.ID)
		testutil.Equal(t, n, 1)
	})
	t.Run("resolved item", func(t *testing.T) {
		testutil.NoError(t, d.ResolveHeraBoardItem(c.ID, i.ID, "agent", ""))
		rr := doBoard(s, "PUT", base, fmt.Sprintf(`{"read":true,"expected_updated_at":%q}`, ts(i.UpdatedAt)))
		testutil.Equal(t, rr.Code, 409)
		testutil.Equal(t, codeOf(t, rr), "item_resolved")
	})
	t.Run("unauthenticated", func(t *testing.T) {
		rr := httptest.NewRecorder()
		unauthed(s).ServeHTTP(rr, httptest.NewRequest("PUT", base, strings.NewReader(`{}`)))
		testutil.Equal(t, rr.Code, 401)
	})
}

func TestBoardDismiss(t *testing.T) {
	s, d, c, _, i := boardRESTFixture(t)
	sink := &boardSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	path := fmt.Sprintf("/api/hera/roles/%d/board/%d/dismiss", c.ID, i.ID)
	body := fmt.Sprintf(`{"expected_updated_at":%q,"note":"nope"}`, ts(i.UpdatedAt))

	rr := doBoard(s, "POST", path, body)
	testutil.Equal(t, rr.Code, 200)
	var got struct {
		Item   db.HeraBoardItem `json:"item"`
		Unread int              `json:"unread_count"`
	}
	testutil.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	testutil.Equal(t, got.Item.Resolved, true)
	testutil.Equal(t, got.Item.ResolvedBy, "human")
	testutil.Equal(t, got.Item.ResolutionNote, "nope")
	testutil.Equal(t, got.Unread, 0)

	testutil.Equal(t, doBoard(s, "POST", path, body).Code, 200)
	notices, err := d.PendingHeraBoardNotices()
	testutil.NoError(t, err)
	_ = notices
	var n int
	for _, m := range mustMsgs(t, d, c.ID) {
		if m.FromRoleID == m.ToRoleID {
			n++
		}
	}
	testutil.Equal(t, n, 1)
	testutil.Equal(t, sink.count(), 1)

	t.Run("malformed and limits", func(t *testing.T) {
		for _, b := range []string{`{`, `{"note":"x"}`, `{"expected_updated_at":"bad"}`,
			fmt.Sprintf(`{"expected_updated_at":%q,"note":%q}`, ts(i.UpdatedAt), strings.Repeat("x", 64*1024+1))} {
			rr := doBoard(s, "POST", path, b)
			testutil.Equal(t, rr.Code, 400)
			testutil.Equal(t, codeOf(t, rr), "invalid_request")
		}
	})
	t.Run("stale revision conflicts", func(t *testing.T) {
		i2, err := d.PostHeraBoardItem(c.ID, "decision", "Two", "b")
		testutil.NoError(t, err)
		_, err = d.UpdateHeraBoardItem(c.ID, i2.ID, "decision", "Two", "changed")
		testutil.NoError(t, err)
		rr := doBoard(s, "POST", fmt.Sprintf("/api/hera/roles/%d/board/%d/dismiss", c.ID, i2.ID), fmt.Sprintf(`{"expected_updated_at":%q}`, ts(i2.UpdatedAt)))
		testutil.Equal(t, rr.Code, 409)
		testutil.Equal(t, codeOf(t, rr), "revision_conflict")
		unread, _, _ := d.HeraBoardUnread(c.ID)
		testutil.Equal(t, unread, 1)
	})
	t.Run("ownership and auth", func(t *testing.T) {
		rr := doBoard(s, "POST", fmt.Sprintf("/api/hera/roles/9999/board/%d/dismiss", i.ID), body)
		testutil.Equal(t, rr.Code, 404)
		rr = httptest.NewRecorder()
		unauthed(s).ServeHTTP(rr, httptest.NewRequest("POST", path, strings.NewReader(body)))
		testutil.Equal(t, rr.Code, http.StatusUnauthorized)
	})
	t.Run("persistence failure never reports success", func(t *testing.T) {
		i3, err := d.PostHeraBoardItem(c.ID, "fyi", "Three", "b")
		testutil.NoError(t, err)
		testutil.NoError(t, d.Close())
		rr := doBoard(s, "POST", fmt.Sprintf("/api/hera/roles/%d/board/%d/dismiss", c.ID, i3.ID), fmt.Sprintf(`{"expected_updated_at":%q}`, ts(i3.UpdatedAt)))
		testutil.Equal(t, rr.Code, 500)
	})
}

func mustMsgs(t *testing.T, d *db.DB, roleID int64) []*db.HeraMessage {
	t.Helper()
	m, err := d.HeraInbox(roleID)
	testutil.NoError(t, err)
	return m
}

func unauthed(s *Server) http.Handler {
	return authMiddleware("test-token", nil, nil, s.routes(), "/public")
}

func TestBoardDismissEscapedNoteLimit(t *testing.T) {
	for _, tc := range []struct {
		name, note string
		status     int
	}{
		{"quotes", strings.Repeat("\"", 64*1024), 200},
		{"backslashes", strings.Repeat("\\", 64*1024), 200},
		{"controls", strings.Repeat("\x00", 64*1024), 200},
		{"decoded too large", strings.Repeat("a", 64*1024+1), 400},
		{"escaped decoded too large", strings.Repeat("\x00", 64*1024+1), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d, c, _, i := boardRESTFixture(t)
			body, err := json.Marshal(map[string]string{"expected_updated_at": ts(i.UpdatedAt), "note": tc.note})
			testutil.NoError(t, err)
			rr := doBoard(s, "POST", fmt.Sprintf("/api/hera/roles/%d/board/%d/dismiss", c.ID, i.ID), string(body))
			testutil.Equal(t, rr.Code, tc.status)
			if rr.Code != tc.status {
				return
			}
			all, err := d.ListHeraBoardItems(c.ID, true)
			testutil.NoError(t, err)
			testutil.Equal(t, all[0].Resolved, tc.status == 200)
			if tc.status == 200 {
				testutil.Equal(t, all[0].ResolutionNote, tc.note)
				notices, err := d.HeraInbox(c.ID)
				testutil.NoError(t, err)
				testutil.Equal(t, len(notices), 1)
				testutil.Contains(t, notices[0].Body, tc.note)
			} else {
				testutil.Equal(t, codeOf(t, rr), "invalid_request")
				testutil.Contains(t, rr.Body.String(), "note exceeds 64 KiB")
			}
		})
	}
}

func TestBoardDismissTransportLimit(t *testing.T) {
	s, d, c, _, i := boardRESTFixture(t)
	body := strings.Repeat(" ", 512*1024) + `{"expected_updated_at":"` + ts(i.UpdatedAt) + `","note":"valid"}`
	rr := doBoard(s, "POST", fmt.Sprintf("/api/hera/roles/%d/board/%d/dismiss", c.ID, i.ID), body)
	testutil.Equal(t, rr.Code, 400)
	testutil.Equal(t, codeOf(t, rr), "invalid_request")
	items, err := d.ListHeraBoardItems(c.ID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Read, false)
	notices, err := d.HeraInbox(c.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(notices), 0)
}
