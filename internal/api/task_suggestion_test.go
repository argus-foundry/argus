package api

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func postBridgeEvent(t *testing.T, s *Server, id string, ev map[string]any) (int, string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"events": []map[string]any{ev}})
	testutil.NoError(t, err)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+id+"/bridge/events", string(b)))
	return w.Code, w.Body.String()
}

func getBridge(t *testing.T, s *Server, id string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/"+id+"/bridge", ""))
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

var suggestSeq int

func suggestEv(typ string, extra map[string]any) map[string]any {
	suggestSeq++
	ev := map[string]any{"type": typ, "generation": "launch", "session_id": "session", "boot": "b", "seq": suggestSeq}
	for k, v := range extra {
		ev[k] = v
	}
	return ev
}

func suggestionSink(t *testing.T) *ctxSink {
	t.Helper()
	sink := &ctxSink{}
	prev := events.SetSink(sink)
	t.Cleanup(func() { events.SetSink(prev) })
	return sink
}

func TestTaskSuggestion_ShapeStateAndEvent(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	sink := suggestionSink(t)
	prompt := task.Prompt

	code, _ := postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": "Run the focused tests"}))
	testutil.Equal(t, code, 200)

	code, body := getBridge(t, s, task.ID)
	testutil.Equal(t, code, 200)
	sg := body["suggestion"].(map[string]any)
	eqAny(t, sg["text"], "Run the focused tests")
	eqAny(t, sg["session_id"], "session")
	eqAny(t, sg["generation"], "launch")
	_, err := time.Parse(time.RFC3339Nano, sg["recorded_at"].(string))
	testutil.NoError(t, err)

	st, ok := s.bridgeHub.Get(task.ID)
	testutil.True(t, ok)
	testutil.Equal(t, st.Suggestion.Text, "Run the focused tests")

	evs := sink.ofType(model.EventTypeTaskSuggestionChanged)
	testutil.Equal(t, len(evs), 1)
	testutil.Equal(t, evs[0].TaskID, task.ID)
	testutil.False(t, strings.Contains(string(evs[0].Payload), "focused"))
	var payload map[string]any
	testutil.NoError(t, json.Unmarshal(evs[0].Payload, &payload))
	testutil.Equal(t, len(payload), 3)
	eqAny(t, payload["session_id"], "session")
	eqAny(t, payload["available"], true)
	eqAny(t, payload["recorded_at"], sg["recorded_at"])

	again, err := s.db.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, again.Prompt, prompt)
}

func suggestionOf(t *testing.T, s *Server, id string) any {
	t.Helper()
	code, body := getBridge(t, s, id)
	testutil.Equal(t, code, 200)
	v, present := body["suggestion"]
	testutil.True(t, present)
	return v
}

func TestTaskSuggestion_ReplacementEmitsEvent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _, task := bridgeAuthorityServer(t)
	sink := suggestionSink(t)
	for _, text := range []string{"first prompt", "replacement prompt"} {
		code, _ := postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": text}))
		testutil.Equal(t, code, 200)
	}
	evs := sink.ofType(model.EventTypeTaskSuggestionChanged)
	testutil.Equal(t, len(evs), 2)
	if len(evs) != 2 {
		t.FailNow()
	}
	var payload map[string]any
	testutil.NoError(t, json.Unmarshal(evs[1].Payload, &payload))
	testutil.Equal(t, evs[1].TaskID, task.ID)
	testutil.Equal(t, len(payload), 3)
	eqAny(t, payload["available"], true)
	eqAny(t, payload["session_id"], "session")
	suggestion := suggestionOf(t, s, task.ID).(map[string]any)
	eqAny(t, suggestion["text"], "replacement prompt")
	eqAny(t, payload["recorded_at"], suggestion["recorded_at"])
	testutil.False(t, strings.Contains(string(evs[1].Payload), "prompt"))
}

func TestTaskSuggestion_ClearsAndInvalidates(t *testing.T) {
	tests := []struct {
		name  string
		clear func(t *testing.T, s *Server, r *bridgeTestRunner, id string)
	}{
		{"explicit clear", func(t *testing.T, s *Server, _ *bridgeTestRunner, id string) {
			code, _ := postBridgeEvent(t, s, id, suggestEv("suggestion.cleared", nil))
			testutil.Equal(t, code, 200)
		}},
		{"new turn", func(t *testing.T, s *Server, _ *bridgeTestRunner, id string) {
			code, _ := postBridgeEvent(t, s, id, suggestEv("turn.start", nil))
			testutil.Equal(t, code, 200)
		}},
		{"session replacement", func(t *testing.T, s *Server, _ *bridgeTestRunner, id string) {
			code, _ := postBridgeEvent(t, s, id, suggestEv("heartbeat", map[string]any{"session_id": "other"}))
			testutil.Equal(t, code, 200)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			s, r, task := bridgeAuthorityServer(t)
			sink := suggestionSink(t)
			code, _ := postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": "next"}))
			testutil.Equal(t, code, 200)
			clearStarted := time.Now()
			tt.clear(t, s, r, task.ID)
			clearFinished := time.Now()

			testutil.True(t, suggestionOf(t, s, task.ID) == nil)
			st, _ := s.bridgeHub.Get(task.ID)
			testutil.True(t, st.Suggestion == nil)

			evs := sink.ofType(model.EventTypeTaskSuggestionChanged)
			testutil.Equal(t, len(evs), 2)
			var payload map[string]any
			testutil.NoError(t, json.Unmarshal(evs[1].Payload, &payload))
			testutil.Equal(t, evs[1].TaskID, task.ID)
			eqAny(t, payload["session_id"], "session")
			eqAny(t, payload["available"], false)
			testutil.Equal(t, len(payload), 3)
			recordedAt, err := time.Parse(time.RFC3339Nano, payload["recorded_at"].(string))
			testutil.NoError(t, err)
			testutil.True(t, !recordedAt.Before(clearStarted) && !recordedAt.After(clearFinished))
		})
	}
}

func TestTaskSuggestion_NoSuggestionIsNullAndSilent(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	sink := suggestionSink(t)
	code, _ := postBridgeEvent(t, s, task.ID, suggestEv("heartbeat", nil))
	testutil.Equal(t, code, 200)
	code, _ = postBridgeEvent(t, s, task.ID, suggestEv("suggestion.cleared", nil))
	testutil.Equal(t, code, 200)
	testutil.True(t, suggestionOf(t, s, task.ID) == nil)
	testutil.Equal(t, len(sink.ofType(model.EventTypeTaskSuggestionChanged)), 0)
}

func TestTaskSuggestion_StaleGenerationCannotReplace(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	sink := suggestionSink(t)
	code, _ := postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": "current"}))
	testutil.Equal(t, code, 200)

	code, body := postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": "stale", "generation": "old-launch"}))
	testutil.Equal(t, code, 200)
	testutil.True(t, strings.Contains(body, `"accepted":0`))
	code, body = postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": "legacy", "generation": ""}))
	testutil.Equal(t, code, 200)
	testutil.True(t, strings.Contains(body, `"accepted":0`))

	_, got := getBridge(t, s, task.ID)
	eqAny(t, got["suggestion"].(map[string]any)["text"], "current")
	testutil.Equal(t, len(sink.ofType(model.EventTypeTaskSuggestionChanged)), 1)
}

func TestTaskSuggestion_InvalidIngestKeepsValidState(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	sink := suggestionSink(t)
	code, _ := postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": "valid"}))
	testutil.Equal(t, code, 200)
	for name, text := range map[string]string{
		"oversized": strings.Repeat("a", 4097),
		"blank":     "   ",
		"empty":     "",
		"nul":       "a\u0000b",
	} {
		t.Run(name, func(t *testing.T) {
			code, _ := postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": text}))
			testutil.Equal(t, code, 400)
		})
	}
	_, got := getBridge(t, s, task.ID)
	eqAny(t, got["suggestion"].(map[string]any)["text"], "valid")
	testutil.Equal(t, len(sink.ofType(model.EventTypeTaskSuggestionChanged)), 1)
}

func TestTaskSuggestion_LostAuthorityReadsNull(t *testing.T) {
	s, r, task := bridgeAuthorityServer(t)
	code, _ := postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": "next"}))
	testutil.Equal(t, code, 200)
	r.session.generation = "restarted"
	code, body := getBridge(t, s, task.ID)
	testutil.Equal(t, code, 200)
	eqAny(t, body["live"], false)
	testutil.True(t, body["suggestion"] == nil)
}

func TestTaskSuggestion_WireUnicode(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{"invalid UTF-8", "\"a\xffb\"", ""},
		{"lone high surrogate", `"a\ud800b"`, ""},
		{"lone low surrogate", `"a\udc00b"`, ""},
		{"reversed surrogates", `"\udc00\ud800"`, ""},
		{"high surrogate before escape", `"\ud800\n"`, ""},
		{"high surrogate before BMP", `"\ud800\u0061"`, ""},
		{"double high surrogate", `"\ud800\ud800"`, ""},
		{"surrogate pair", `"a\ud83d\ude80b"`, "a🚀b"},
		{"first surrogate pair", `"\ud800\udc00"`, "\U00010000"},
		{"first surrogate pair raw", "\"\U00010000\"", "\U00010000"},
		{"last surrogate pair", `"\udbff\udfff"`, "\U0010ffff"},
		{"last surrogate pair raw", "\"\U0010ffff\"", "\U0010ffff"},
		{"emoji modifier upper low surrogate", `"\ud83c\udfff"`, "\U0001f3ff"},
		{"emoji modifier raw", "\"\U0001f3ff\"", "\U0001f3ff"},
		{"literal replacement rune", `"a�b"`, "a�b"},
		{"escaped replacement rune", `"a\ufffdb"`, "a�b"},
		{"escaped backslash", `"a\\ud800b"`, `a\ud800b`},
		{"escaped slash", `"a\/b"`, "a/b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			s, _, task := bridgeAuthorityServer(t)
			sink := suggestionSink(t)
			code, _ := postBridgeEvent(t, s, task.ID, suggestEv("suggestion.changed", map[string]any{"text": "valid"}))
			testutil.Equal(t, code, 200)
			before, ok := s.bridgeHub.Get(task.ID)
			testutil.True(t, ok)
			body := `{"events":[{"type":"turn.start","generation":"launch","session_id":"session","boot":"b","seq":1000000},` +
				`{"type":"suggestion.changed","generation":"launch","session_id":"session","boot":"b","seq":1000001,"text":` + tc.text + `}]}`
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", body))
			after, ok := s.bridgeHub.Get(task.ID)
			testutil.True(t, ok)
			if tc.want == "" {
				testutil.Equal(t, w.Code, 400)
				testutil.DeepEqual(t, after, before)
				testutil.Equal(t, len(sink.ofType(model.EventTypeTaskSuggestionChanged)), 1)
				_, got := getBridge(t, s, task.ID)
				eqAny(t, got["suggestion"].(map[string]any)["text"], "valid")
			} else {
				testutil.Equal(t, w.Code, 200)
				testutil.Equal(t, after.Suggestion.Text, tc.want)
				testutil.True(t, after.Busy)
			}
		})
	}
}

func TestTaskSuggestion_BoundariesAndBatch(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	code, _ := getBridge(t, s, task.ID)
	testutil.Equal(t, code, 404)

	h := authMiddleware(s.token, s.db, s.push, s.routes(), "/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/tasks/"+task.ID+"/bridge", nil))
	testutil.Equal(t, w.Code, 401)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/tasks/"+task.ID+"/bridge/events", strings.NewReader("{}")))
	testutil.Equal(t, w.Code, 401)

	// A whole batch carrying one invalid suggestion applies nothing.
	body := `{"events":[{"type":"turn.start","generation":"launch","session_id":"session","boot":"b","seq":` + strconv.Itoa(1000) +
		`},{"type":"suggestion.changed","generation":"launch","session_id":"session","boot":"b","seq":1001,"text":""}]}`
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", body))
	testutil.Equal(t, w.Code, 400)
	_, ok := s.bridgeHub.Get(task.ID)
	testutil.False(t, ok)
}
