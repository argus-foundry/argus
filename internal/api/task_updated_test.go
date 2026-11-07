package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
