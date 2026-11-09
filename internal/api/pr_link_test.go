package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

const linkURL = "https://github.com/org/repo/pull/7"

func prReq(srv *Server, method, id, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq(method, "/api/tasks/"+id+"/pr", body))
	return w
}

func prChangedPayloads(t *testing.T, sink *recordingSink) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, ev := range sink.events() {
		if ev.Type != model.EventTypeTaskPRChanged {
			continue
		}
		var p map[string]string
		testutil.NoError(t, json.Unmarshal(ev.Payload, &p))
		out = append(out, p)
	}
	return out
}

func TestTaskPRLink_PutGetDelete(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "l1", Name: "n", Branch: "b"}))
	sink := installAPISink(t)
	testutil.NoError(t, d.SetMetaBatch("l1", "pr", map[string]string{"state": "approved", "url": "https://github.com/org/repo/pull/1", "lifecycle": "open"}))

	w := prReq(srv, "PUT", "l1", `{"url":"`+linkURL+`/?x=1"}`)
	testutil.Equal(t, w.Code, http.StatusOK)
	var resp map[string]any
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	testutil.DeepEqual(t, resp, map[string]any{
		"source": "manual", "url": linkURL, "state": "unknown", "lifecycle": "unknown", "checked_at": nil,
	})
	stored, err := d.ManualPR("l1")
	testutil.NoError(t, err)
	testutil.Equal(t, stored, linkURL)
	entries, err := d.ListMeta("l1", "pr")
	testutil.NoError(t, err)
	testutil.Equal(t, len(entries), 0)
	testutil.DeepEqual(t, prChangedPayloads(t, sink), []map[string]string{
		{"source": "manual", "url": linkURL, "state": "unknown", "lifecycle": "unknown"},
	})
	testutil.DeepEqual(t, updatedEvents(t, sink), [][]string{{"pr"}})

	wrote, err := d.SetManualPRMetaIfChanged("l1", linkURL, map[string]string{"state": "awaiting-review", "url": linkURL, "lifecycle": "open"})
	testutil.NoError(t, err)
	testutil.Equal(t, wrote, true)
	w = prReq(srv, "GET", "l1", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	resp = nil
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	testutil.Equal(t, resp["source"], "manual")
	testutil.Equal(t, resp["state"], "awaiting-review")
	testutil.Equal(t, resp["lifecycle"], "open")
	testutil.Equal(t, resp["checked_at"] != nil, true)

	w = prReq(srv, "DELETE", "l1", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	resp = nil
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	testutil.Equal(t, resp["source"], "automatic")
	testutil.Equal(t, resp["state"], "none")
	stored, err = d.ManualPR("l1")
	testutil.NoError(t, err)
	testutil.Equal(t, stored, "")
	entries, err = d.ListMeta("l1", "pr")
	testutil.NoError(t, err)
	testutil.Equal(t, len(entries), 0)
	pc := prChangedPayloads(t, sink)
	testutil.Equal(t, len(pc), 2)
	testutil.DeepEqual(t, pc[1], map[string]string{"source": "automatic", "url": "", "state": "none", "lifecycle": "unknown"})
}

func TestTaskPRLink_InvalidURLLeavesAssociationUnchanged(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "i1", Name: "n"}))
	testutil.NoError(t, d.SetManualPR("i1", linkURL))
	sink := installAPISink(t)
	for _, body := range []string{
		`{"url":"https://evil.com/org/repo/pull/7"}`, `{"url":"http://github.com/org/repo/pull/7"}`,
		`{"url":"https://github.com/org/repo/pull/0"}`, `{"url":""}`, `{}`, `nope`,
		`{"url":"` + linkURL + `","extra":1}`,
	} {
		w := prReq(srv, "PUT", "i1", body)
		testutil.Equal(t, w.Code, http.StatusBadRequest)
	}
	stored, err := d.ManualPR("i1")
	testutil.NoError(t, err)
	testutil.Equal(t, stored, linkURL)
	testutil.Equal(t, len(sink.events()), 0)
}

func TestTaskPRLink_MissingTask(t *testing.T) {
	srv, _ := testServer(t)
	for _, m := range []string{"GET", "DELETE"} {
		testutil.Equal(t, prReq(srv, m, "nope", "").Code, http.StatusNotFound)
	}
	testutil.Equal(t, prReq(srv, "PUT", "nope", `{"url":"`+linkURL+`"}`).Code, http.StatusNotFound)
}

func TestTaskPRLink_AutomaticProjection(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "g1", Name: "n"}))
	var resp map[string]any
	w := prReq(srv, "GET", "g1", "")
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	testutil.DeepEqual(t, resp, map[string]any{
		"source": "automatic", "url": "", "state": "none", "lifecycle": "unknown", "checked_at": nil,
	})
}

func TestTaskPRLink_RequiresAuth(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{ID: "u1", Name: "n"}))
	h := authMiddleware(srv.token, srv.db, srv.push, srv.routes(), "/")
	for _, m := range []string{"GET", "PUT", "DELETE"} {
		req := httptest.NewRequest(m, "/api/tasks/u1/pr", strings.NewReader(`{"url":"`+linkURL+`"}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		testutil.Equal(t, w.Code, http.StatusUnauthorized)
	}
	stored, err := d.ManualPR("u1")
	testutil.NoError(t, err)
	testutil.Equal(t, stored, "")
}
