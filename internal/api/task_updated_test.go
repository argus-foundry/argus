package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func updatedEvents(t *testing.T, sink *recordingSink) [][]string {
	t.Helper()
	var out [][]string
	for _, ev := range sink.events() {
		if ev.Type != model.EventTypeTaskUpdated {
			continue
		}
		var p struct {
			Changed []string `json:"changed_fields"`
		}
		testutil.NoError(t, json.Unmarshal(ev.Payload, &p))
		out = append(out, p.Changed)
	}
	return out
}

func installAPISink(t *testing.T) *recordingSink {
	t.Helper()
	sink := &recordingSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	return sink
}

func TestHandlePinTask(t *testing.T) {
	post := func(srv *Server, id, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+id+"/pin", body))
		return w
	}

	t.Run("pin and unpin", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "p1", Name: "n"}))
		sink := installAPISink(t)

		w := post(srv, "p1", `{"pinned":true}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		var resp map[string]any
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		testutil.DeepEqual(t, resp, map[string]any{"task_id": "p1", "pinned": true, "archived": false})
		got, _ := d.Get("p1")
		testutil.Equal(t, got.Pinned, true)

		w = post(srv, "p1", `{"pinned":false}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		got, _ = d.Get("p1")
		testutil.Equal(t, got.Pinned, false)
		testutil.DeepEqual(t, updatedEvents(t, sink), [][]string{{"pinned"}, {"pinned"}})
	})

	t.Run("pinning restores archived task", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "p2", Name: "n", Archived: true}))
		sink := installAPISink(t)
		w := post(srv, "p2", `{"pinned":true}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Equal(t, strings.Contains(w.Body.String(), `"archived":false`), true)
		got, _ := d.Get("p2")
		testutil.Equal(t, got.Archived, false)
		testutil.DeepEqual(t, updatedEvents(t, sink), [][]string{{"archived", "pinned"}})
	})

	t.Run("archiving clears pin in either order", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "p3", Name: "n"}))
		post(srv, "p3", `{"pinned":true}`)
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/p3/archive", ""))
		testutil.Equal(t, w.Code, http.StatusOK)
		got, _ := d.Get("p3")
		testutil.Equal(t, got.Archived, true)
		testutil.Equal(t, got.Pinned, false)
		post(srv, "p3", `{"pinned":true}`)
		got, _ = d.Get("p3")
		testutil.Equal(t, got.Archived, false)
		testutil.Equal(t, got.Pinned, true)
	})

	t.Run("preserves unrelated edits and does not touch runner", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "p4", Name: "n", Model: "m", SessionID: "s", Status: model.StatusInProgress}))
		testutil.NoError(t, d.Rename("p4", "renamed-meanwhile"))
		testutil.NoError(t, d.SetStatus("p4", model.StatusInReview))
		w := post(srv, "p4", `{"pinned":true}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		got, _ := d.Get("p4")
		testutil.Equal(t, got.Name, "renamed-meanwhile")
		testutil.Equal(t, got.Status, model.StatusInReview)
		testutil.Equal(t, got.Model, "m")
		testutil.Equal(t, got.SessionID, "s")
		testutil.Equal(t, len(srv.runner.Running()), 0)
	})

	t.Run("no-op emits no event", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "p5", Name: "n", Pinned: true}))
		sink := installAPISink(t)
		w := post(srv, "p5", `{"pinned":true}`)
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.Equal(t, len(updatedEvents(t, sink)), 0)
	})

	t.Run("errors", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "p6", Name: "n"}))
		sink := installAPISink(t)
		for _, tc := range []struct {
			name, id, body string
			code           int
		}{
			{"malformed", "p6", `{`, 400},
			{"missing pinned", "p6", `{}`, 400},
			{"null pinned", "p6", `{"pinned":null}`, 400},
			{"non-boolean", "p6", `{"pinned":"yes"}`, 400},
			{"unknown field", "p6", `{"pinned":true,"name":"x"}`, 400},
			{"oversized", "p6", `{"pinned":true}` + strings.Repeat(" ", 1024), 400},
			{"empty body", "p6", ``, 400},
			{"missing task", "nope", `{"pinned":true}`, 404},
		} {
			t.Run(tc.name, func(t *testing.T) {
				testutil.Equal(t, post(srv, tc.id, tc.body).Code, tc.code)
			})
		}
		got, _ := d.Get("p6")
		testutil.Equal(t, got.Pinned, false)
		testutil.Equal(t, len(updatedEvents(t, sink)), 0)
	})

	t.Run("requires auth", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "p7", Name: "n"}))
		h := authMiddleware(srv.token, srv.db, srv.push, srv.routes(), "/")
		req := httptest.NewRequest("POST", "/api/tasks/p7/pin", strings.NewReader(`{"pinned":true}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		testutil.Equal(t, w.Code, http.StatusUnauthorized)
		got, _ := d.Get("p7")
		testutil.Equal(t, got.Pinned, false)
	})
}

func TestHandlePinTaskWholeBody(t *testing.T) {
	const body = `{"pinned":true}`
	for _, tc := range []struct {
		name, body string
		code       int
	}{
		{"garbage", body + "garbage", http.StatusBadRequest},
		{"second object", body + `{"pinned":false}`, http.StatusBadRequest},
		{"second null", body + "null", http.StatusBadRequest},
		{"trailing whitespace over limit", body + strings.Repeat(" ", 1025-len(body)), http.StatusBadRequest},
		{"leading whitespace over limit", strings.Repeat(" ", 1025-len(body)) + body, http.StatusBadRequest},
		{"internal whitespace over limit", `{"pinned":` + strings.Repeat(" ", 1024) + `true}`, http.StatusBadRequest},
		{"exact limit", body + strings.Repeat(" ", 1024-len(body)), http.StatusOK},
		{"whitespace within limit", body + " \t\r\n", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, d := testServer(t)
			testutil.NoError(t, d.Add(&model.Task{ID: "body", Name: "n", Archived: true}))
			before, err := d.Get("body")
			testutil.NoError(t, err)
			sink := installAPISink(t)
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/body/pin", tc.body))
			testutil.Equal(t, w.Code, tc.code)
			after, err := d.Get("body")
			testutil.NoError(t, err)
			if tc.code == http.StatusBadRequest {
				testutil.DeepEqual(t, after, before)
				testutil.Equal(t, len(sink.events()), 0)
			} else {
				testutil.Equal(t, after.Pinned, true)
				testutil.Equal(t, after.Archived, false)
				testutil.DeepEqual(t, updatedEvents(t, sink), [][]string{{"archived", "pinned"}})
			}
		})
	}
}

func TestRESTMutationsEmitTaskUpdated(t *testing.T) {
	t.Run("archive and restore", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "a1", Name: "n"}))
		sink := installAPISink(t)
		for _, path := range []string{"archive", "unarchive"} {
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/a1/"+path, ""))
			testutil.Equal(t, w.Code, http.StatusOK)
		}
		testutil.DeepEqual(t, updatedEvents(t, sink), [][]string{{"archived"}, {"archived"}})
		got, _ := d.Get("a1")
		testutil.Equal(t, got.Archived, false)
	})

	t.Run("rename and status", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "a2", Name: "n"}))
		sink := installAPISink(t)
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/a2/rename", `{"name":"x"}`))
		testutil.Equal(t, w.Code, http.StatusOK)
		w = httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/a2/status", `{"status":"in_review"}`))
		testutil.Equal(t, w.Code, http.StatusOK)
		testutil.DeepEqual(t, updatedEvents(t, sink), [][]string{{"name"}, {"status"}})
	})

	t.Run("failed write publishes nothing", func(t *testing.T) {
		srv, _ := testServer(t)
		sink := installAPISink(t)
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/ghost/archive", ""))
		testutil.Equal(t, w.Code, http.StatusNotFound)
		w = httptest.NewRecorder()
		srv.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/ghost/rename", `{"name":"x"}`))
		testutil.Equal(t, w.Code, http.StatusNotFound)
		testutil.Equal(t, len(sink.events()), 0)
	})

	t.Run("raw PUT emits only changed fields", func(t *testing.T) {
		srv, d := testServer(t)
		testutil.NoError(t, d.Add(&model.Task{ID: "a3", Name: "n"}))
		sink := installAPISink(t)
		put := func(mut func(*model.Task)) {
			cur, _ := d.Get("a3")
			mut(cur)
			b, _ := json.Marshal(cur)
			w := httptest.NewRecorder()
			srv.routes().ServeHTTP(w, authedReq("PUT", "/api/tasks/a3/raw", string(b)))
			testutil.Equal(t, w.Code, http.StatusOK)
		}
		put(func(*model.Task) {})
		testutil.Equal(t, len(updatedEvents(t, sink)), 0)
		put(func(x *model.Task) { x.Result = `{"ok":true}`; x.SetPinned(true) })
		testutil.DeepEqual(t, updatedEvents(t, sink), [][]string{{"pinned", "result"}})
		for _, ev := range sink.events() {
			if ev.Type == model.EventTypeTaskUpdated {
				testutil.Equal(t, strings.Contains(string(ev.Payload), "ok"), false)
			}
		}
	})
}

func TestTaskUpdatedReachesSecondSSEClient(t *testing.T) {
	srv, d := testServer(t)
	hs := httptest.NewServer(authMiddleware(srv.token, srv.db, srv.push, srv.routes(), "/"))
	t.Cleanup(hs.Close)
	prev := events.SetSink(srv)
	t.Cleanup(func() { events.SetSink(prev) })
	testutil.NoError(t, d.Add(&model.Task{ID: "s1", Name: "n"}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", hs.URL+"/api/events/stream", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	testutil.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	scanner := bufio.NewScanner(resp.Body)
	time.Sleep(100 * time.Millisecond)

	preq, _ := http.NewRequest("POST", hs.URL+"/api/tasks/s1/pin", strings.NewReader(`{"pinned":true}`))
	preq.Header.Set("Authorization", "Bearer test-token")
	presp, err := http.DefaultClient.Do(preq)
	testutil.NoError(t, err)
	presp.Body.Close()
	testutil.Equal(t, presp.StatusCode, http.StatusOK)

	var found model.Event
	for i := 0; i < 5 && found.Type == ""; i++ {
		typ, data, rerr := readSSEEvent(scanner)
		if rerr != nil {
			break
		}
		if typ == "task.updated" {
			testutil.NoError(t, json.Unmarshal([]byte(data), &found))
		}
	}
	testutil.Equal(t, found.Type, "task.updated")
	testutil.Equal(t, model.EventTypeTaskUpdated, "task.updated")
	testutil.Equal(t, found.TaskID, "s1")
	testutil.Equal(t, string(found.Payload), `{"changed_fields":["pinned"]}`)
	got, _ := d.Get("s1")
	testutil.Equal(t, got.Pinned, true)
}
