package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/hera"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type lcSessions struct {
	mu     sync.Mutex
	alive  map[string]bool
	writes []string
}

func (s *lcSessions) SessionAlive(id string) bool { return s.alive[id] }
func (s *lcSessions) SendSystemInput(id, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, id)
	return nil
}

type lcRecycler struct{ restarts int }

func (r *lcRecycler) IsIdle(string) bool              { return true }
func (r *lcRecycler) StopStrayJobs(_, _ string) error { return nil }
func (r *lcRecycler) Restart(_ string, _ int64) error { r.restarts++; return nil }

type failingCreateStore struct{ *db.DB }

func (failingCreateStore) CreateOperation(*db.Operation) (*db.Operation, bool, error) {
	return nil, false, errors.New("disk full")
}

type captureSink struct {
	mu  sync.Mutex
	evs []model.Event
}

func (c *captureSink) Emit(ev model.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evs = append(c.evs, ev)
}

const (
	lcReqA = "11111111-1111-4111-8111-111111111111"
	lcReqB = "22222222-2222-4222-8222-222222222222"
)

func lifecycleFixture(t *testing.T) (*Server, *db.DB, *lcSessions, *lcRecycler, []*db.HeraRole, []*model.Task) {
	t.Helper()
	srv, d := testServer(t)
	_, roles, tasks := seedOrch(t, d, "lc", db.HeraKindWorker, db.HeraKindCoordinator)
	sess := &lcSessions{alive: map[string]bool{tasks[0].ID: true, tasks[1].ID: true}}
	rec := &lcRecycler{}
	svc := hera.NewLifecycleService(d, sess, rec)
	svc.SetSynchronous()
	srv.SetHeraLifecycle(svc)
	return srv, d, sess, rec, roles, tasks
}

func lcPost(srv *Server, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("POST", path, body))
	return w
}

func lcDecode(t *testing.T, w *httptest.ResponseRecorder) hera.LifecycleOperation {
	t.Helper()
	var op hera.LifecycleOperation
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &op))
	return op
}

func TestHeraBounce_API(t *testing.T) {
	t.Run("accepts, replays idempotently, serves the operation and emits events", func(t *testing.T) {
		sink := &captureSink{}
		prev := events.SetSink(sink)
		t.Cleanup(func() { events.SetSink(prev) })
		srv, _, sess, rec, roles, tasks := lifecycleFixture(t)

		w := lcPost(srv, idp("roles", roles[0].ID, "bounce"), `{"request_id":"`+lcReqA+`"}`)
		testutil.Equal(t, w.Code, http.StatusAccepted)
		op := lcDecode(t, w)
		testutil.Equal(t, op.Kind, "bounce")
		testutil.Equal(t, op.State, "awaiting_handoff")
		testutil.Equal(t, op.TaskID, tasks[0].ID)
		testutil.Equal(t, op.RoleID, roles[0].ID)
		testutil.True(t, op.Error == nil)

		w = lcPost(srv, idp("roles", roles[0].ID, "bounce"), `{"request_id":"`+lcReqA+`"}`)
		testutil.Equal(t, w.Code, http.StatusAccepted)
		testutil.Equal(t, lcDecode(t, w).OperationID, op.OperationID)
		testutil.Equal(t, len(sess.writes), 1)
		testutil.Equal(t, rec.restarts, 0)

		g := httptest.NewRecorder()
		srv.routes().ServeHTTP(g, authedReq("GET", idp("roles", roles[0].ID, "lifecycle-operations/"+op.OperationID), ""))
		testutil.Equal(t, g.Code, http.StatusOK)
		testutil.Contains(t, g.Body.String(), `"error":null`)

		sink.mu.Lock()
		defer sink.mu.Unlock()
		var lc []model.Event
		for _, ev := range sink.evs {
			if ev.Type == model.EventTypeHeraLifecycleChanged {
				lc = append(lc, ev)
			}
		}
		testutil.Equal(t, len(lc), 1)
		testutil.Contains(t, string(lc[0].Payload), `"state":"awaiting_handoff"`)
		testutil.Contains(t, string(lc[0].Payload), `"error":null`)
	})

	t.Run("status codes", func(t *testing.T) {
		srv, _, sess, _, roles, tasks := lifecycleFixture(t)
		bounce := func(id int64, body string) int {
			return lcPost(srv, idp("roles", id, "bounce"), body).Code
		}
		testutil.Equal(t, bounce(roles[0].ID, `not json`), http.StatusBadRequest)
		testutil.Equal(t, bounce(roles[0].ID, `{"request_id":"nope"}`), http.StatusBadRequest)
		testutil.Equal(t, bounce(roles[0].ID, `{"request_id":"`+lcReqA+`","instruction":"rm -rf"}`), http.StatusBadRequest)
		testutil.Equal(t, bounce(roles[0].ID, `{"request_id":"`+lcReqA+`","force":true}`), http.StatusBadRequest)
		testutil.Equal(t, lcPost(srv, "/api/hera/roles/abc/bounce", `{}`).Code, http.StatusBadRequest)
		testutil.Equal(t, bounce(9999, `{"request_id":"`+lcReqA+`"}`), http.StatusNotFound)
		testutil.Equal(t, bounce(roles[1].ID, `{"request_id":"`+lcReqA+`"}`), http.StatusConflict)

		sess.alive[tasks[0].ID] = false
		testutil.Equal(t, bounce(roles[0].ID, `{"request_id":"`+lcReqA+`"}`), http.StatusConflict)
		sess.alive[tasks[0].ID] = true

		testutil.Equal(t, bounce(roles[0].ID, `{"request_id":"`+lcReqA+`"}`), http.StatusAccepted)
		testutil.Equal(t, bounce(roles[0].ID, `{"request_id":"`+lcReqB+`"}`), http.StatusConflict)
		testutil.Equal(t, bounce(roles[0].ID, `{"request_id":"`+lcReqA+`"}`), http.StatusAccepted)
		testutil.Equal(t, len(sess.writes), 1)
		testutil.Equal(t, lcPost(srv, idp("roles", roles[1].ID, "recycle"), `{"request_id":"`+lcReqA+`","force":true}`).Code, http.StatusConflict)
	})

	t.Run("unauthenticated is 401", func(t *testing.T) {
		srv, _, _, _, roles, _ := lifecycleFixture(t)
		req := httptest.NewRequest("POST", idp("roles", roles[0].ID, "bounce"), nil)
		w := httptest.NewRecorder()
		authMiddleware(srv.token, srv.db, srv.push, srv.routes(), "/").ServeHTTP(w, req)
		testutil.Equal(t, w.Code, http.StatusUnauthorized)
	})

	t.Run("persist failure is 500 without leaking detail", func(t *testing.T) {
		srv, d, sess, _, roles, _ := lifecycleFixture(t)
		svc := hera.NewLifecycleService(failingCreateStore{d}, sess, &lcRecycler{})
		srv.SetHeraLifecycle(svc)
		w := lcPost(srv, idp("roles", roles[0].ID, "bounce"), `{"request_id":"`+lcReqA+`"}`)
		testutil.Equal(t, w.Code, http.StatusInternalServerError)
		testutil.True(t, !strings.Contains(w.Body.String(), "disk full"))
		testutil.Equal(t, len(sess.writes), 0)
	})

	t.Run("no service is 503", func(t *testing.T) {
		srv, d := testServer(t)
		_, roles, _ := seedOrch(t, d, "x", db.HeraKindWorker)
		w := lcPost(srv, idp("roles", roles[0].ID, "bounce"), `{"request_id":"`+lcReqA+`"}`)
		testutil.Equal(t, w.Code, http.StatusServiceUnavailable)
	})
}

func TestHeraRecycle_API(t *testing.T) {
	t.Run("forced recycle runs and reports restarted", func(t *testing.T) {
		srv, _, _, rec, roles, tasks := lifecycleFixture(t)
		body := `{"request_id":"` + lcReqA + `","force":true}`
		w := lcPost(srv, idp("roles", roles[1].ID, "recycle"), body)
		testutil.Equal(t, w.Code, http.StatusAccepted)
		op := lcDecode(t, w)
		testutil.Equal(t, op.Kind, "force_recycle")
		testutil.Equal(t, op.TaskID, tasks[1].ID)
		testutil.Equal(t, rec.restarts, 1)

		g := httptest.NewRecorder()
		srv.routes().ServeHTTP(g, authedReq("GET", idp("roles", roles[1].ID, "lifecycle-operations/"+op.OperationID), ""))
		testutil.Equal(t, g.Code, http.StatusOK)
		testutil.Equal(t, lcDecode(t, g).State, "restarted")

		w = lcPost(srv, idp("roles", roles[1].ID, "recycle"), body)
		testutil.Equal(t, w.Code, http.StatusAccepted)
		testutil.Equal(t, rec.restarts, 1)
	})

	t.Run("validation", func(t *testing.T) {
		srv, _, _, _, roles, _ := lifecycleFixture(t)
		rc := func(id int64, body string) int { return lcPost(srv, idp("roles", id, "recycle"), body).Code }
		testutil.Equal(t, rc(roles[1].ID, `{"request_id":"`+lcReqA+`"}`), http.StatusBadRequest)
		testutil.Equal(t, rc(roles[1].ID, `{"request_id":"`+lcReqA+`","force":false}`), http.StatusBadRequest)
		testutil.Equal(t, rc(roles[1].ID, `{"request_id":"x","force":true}`), http.StatusBadRequest)
		testutil.Equal(t, rc(9999, `{"request_id":"`+lcReqA+`","force":true}`), http.StatusNotFound)
		testutil.Equal(t, rc(roles[0].ID, `{"request_id":"`+lcReqA+`","force":true}`), http.StatusConflict)
	})

	t.Run("get scoping and ids", func(t *testing.T) {
		srv, _, _, _, roles, _ := lifecycleFixture(t)
		get := func(path string) int {
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq("GET", path, ""))
			return w.Code
		}
		base := "/api/hera/roles/" + strconv.FormatInt(roles[1].ID, 10) + "/lifecycle-operations/"
		testutil.Equal(t, get(base+"bad"), http.StatusBadRequest)
		testutil.Equal(t, get(base+lcReqB), http.StatusNotFound)
		w := lcPost(srv, idp("roles", roles[1].ID, "recycle"), `{"request_id":"`+lcReqA+`","force":true}`)
		op := lcDecode(t, w)
		testutil.Equal(t, get("/api/hera/roles/"+strconv.FormatInt(roles[0].ID, 10)+"/lifecycle-operations/"+op.OperationID), http.StatusNotFound)
	})
}
