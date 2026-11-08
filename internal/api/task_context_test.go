package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drn/argus/internal/bridge"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type ctxSink struct {
	mu  sync.Mutex
	got []model.Event
}

func (r *ctxSink) Emit(ev model.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, ev)
}

func (r *ctxSink) ofType(typ string) []model.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.Event
	for _, e := range r.got {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func getContext(t *testing.T, s *Server, id string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+id+"/context", ""))
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func postMeasure(t *testing.T, s *Server, id, ctx string, seq int) int {
	t.Helper()
	body := `{"events":[{"type":"session.measure","generation":"launch","session_id":"session","boot":"b","seq":` +
		string(rune('0'+seq)) + `,"context":` + ctx + `}]}`
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+id+"/bridge/events", body))
	return w.Code
}

func TestTaskContext_ShapeCategoriesAndEvent(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	sink := &ctxSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })

	code := postMeasure(t, s, task.ID, `{"tokens":12000,"percent":6,"window":200000,"categories":{"messages":5000,"memory":0,"tools":3000,"system":1000}}`, 1)
	testutil.Equal(t, code, 200)

	code, body := getContext(t, s, task.ID)
	testutil.Equal(t, code, 200)
	eqAny(t, body["task_id"], task.ID)
	eqAny(t, body["session_id"], "session")
	eqAny(t, body["provider"], "anthropic")
	eqAny(t, body["source"], "bridge")
	eqAny(t, body["live"], true)
	_, err := time.Parse(time.RFC3339Nano, body["measured_at"].(string))
	testutil.NoError(t, err)
	c := body["context"].(map[string]any)
	eqAny(t, c["used_tokens"], float64(12000))
	eqAny(t, c["window_tokens"], float64(200000))
	eqAny(t, c["percent"], float64(6))
	cats := c["categories"].(map[string]any)
	testutil.Equal(t, len(cats), 7)
	eqAny(t, cats["messages"], float64(5000))
	eqAny(t, cats["tools"], float64(3000))
	eqAny(t, cats["system"], float64(1000))
	v, present := cats["memory"]
	testutil.True(t, present)
	eqAny(t, v, float64(0)) // measured zero, not unknown
	for _, k := range []string{"agents", "skills", "compaction"} {
		v, present := cats[k]
		testutil.True(t, present)
		testutil.True(t, v == nil)
	}

	st, ok := s.bridgeHub.Get(task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, *st.Context.Categories.Messages, int64(5000))
	testutil.True(t, st.Context.Categories.Agents == nil)

	evs := sink.ofType(model.EventTypeTaskContextChanged)
	testutil.Equal(t, len(evs), 1)
	testutil.Equal(t, evs[0].TaskID, task.ID)
	testutil.False(t, strings.Contains(string(evs[0].Payload), "5000"))
	var payload map[string]string
	testutil.NoError(t, json.Unmarshal(evs[0].Payload, &payload))
	testutil.Equal(t, len(payload), 2)
	testutil.Equal(t, payload["session_id"], "session")
	eqAny(t, payload["measured_at"], body["measured_at"])
}

func TestTaskContext_MissingTaskAndNoMeasurement(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	code, _ := getContext(t, s, "nope")
	testutil.Equal(t, code, 404)

	code, body := getContext(t, s, task.ID)
	testutil.Equal(t, code, 200)
	eqAny(t, body["live"], false)
	testutil.True(t, body["context"] == nil)
	testutil.True(t, body["measured_at"] == nil)
	eqAny(t, body["provider"], "anthropic")
}

func TestTaskContext_RequiresAuth(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	h := authMiddleware(s.token, s.db, s.push, s.routes(), "/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/tasks/"+task.ID+"/context", nil))
	testutil.Equal(t, w.Code, 401)
}

func TestTaskContext_InvalidMeasurementKeepsValidSnapshot(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	sink := &ctxSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	testutil.Equal(t, postMeasure(t, s, task.ID, `{"tokens":100,"window":200000,"categories":{"messages":50}}`, 1), 200)
	for _, bad := range []string{
		`{"tokens":-1,"window":200000}`,
		`{"tokens":10,"window":200000,"categories":{"tools":-5}}`,
		`{"tokens":10,"window":-1}`,
		`{"tokens":10,"percent":-3,"window":200000}`,
	} {
		testutil.Equal(t, postMeasure(t, s, task.ID, bad, 2), 200)
	}
	st, _ := s.bridgeHub.Get(task.ID)
	testutil.Equal(t, *st.Context.Tokens, int64(100))
	testutil.Equal(t, *st.Context.Categories.Messages, int64(50))
	testutil.Equal(t, len(sink.ofType(model.EventTypeTaskContextChanged)), 1)
}

func TestTaskContext_InactiveBridgeReturnsRecordedNotLive(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	old := time.Now().Add(-10 * time.Minute)
	n := s.bridgeHub.Ingest(task.ID, []bridge.Event{{Type: bridge.TypeMeasure, Generation: "launch", SessionID: "session", Boot: "b", Seq: 1,
		Context: &bridge.Context{Tokens: ptr64(7), Window: 100}}}, old)
	testutil.Equal(t, n, 1)
	code, body := getContext(t, s, task.ID)
	testutil.Equal(t, code, 200)
	eqAny(t, body["live"], false)
	c := body["context"].(map[string]any)
	eqAny(t, c["used_tokens"], float64(7))
}

func TestTaskContext_ReplacedSessionNeverAppears(t *testing.T) {
	s, r, task := bridgeAuthorityServer(t)
	testutil.Equal(t, postMeasure(t, s, task.ID, `{"tokens":100,"window":200000}`, 1), 200)
	r.session.generation = "relaunch"
	code, body := getContext(t, s, task.ID)
	testutil.Equal(t, code, 200)
	eqAny(t, body["live"], false)
	testutil.True(t, body["context"] == nil)

	// the replacing process's own report is live and the old figures are gone
	body2 := `{"events":[{"type":"session.measure","generation":"relaunch","session_id":"s2","boot":"b2","seq":1,"context":{"tokens":5,"window":200000}}]}`
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", body2))
	testutil.Equal(t, w.Code, 200)
	_, got := getContext(t, s, task.ID)
	eqAny(t, got["live"], true)
	eqAny(t, got["session_id"], "s2")
	eqAny(t, got["context"].(map[string]any)["used_tokens"], float64(5))
}

func TestTaskContext_NewSessionClearsOldCategories(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	testutil.Equal(t, postMeasure(t, s, task.ID, `{"tokens":100,"window":200000,"categories":{"messages":50}}`, 1), 200)
	body := `{"events":[{"type":"session.measure","generation":"launch","session_id":"other","boot":"b","seq":2,"context":{"tokens":9,"window":200000}}]}`
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", body))
	testutil.Equal(t, w.Code, 200)
	_, got := getContext(t, s, task.ID)
	cats := got["context"].(map[string]any)["categories"].(map[string]any)
	testutil.True(t, cats["messages"] == nil)
}

func ptr64(v int64) *int64 { return &v }

func eqAny(t *testing.T, got, want any) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v (%T), want %v (%T)", got, got, want, want)
	}
}
