package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/voice"
)

type mcpVoiceOp struct{}

func (mcpVoiceOp) Snapshot(context.Context, time.Time) (voice.Snapshot, error) {
	return voice.Snapshot{Counts: voice.Counts{Running: 1}, Running: []string{"Review"}}, nil
}
func (mcpVoiceOp) Find(context.Context, string, string, string) (voice.FindResult, error) {
	return voice.FindResult{}, nil
}
func (mcpVoiceOp) Create(context.Context, string, string, string) (voice.TaskRef, error) {
	return voice.TaskRef{}, nil
}
func (mcpVoiceOp) Stop(context.Context, string) error         { return nil }
func (mcpVoiceOp) Resume(context.Context, string, bool) error { return nil }
func (mcpVoiceOp) Send(context.Context, string, string) error { return nil }

func TestVoiceToolsHiddenUntilWired(t *testing.T) {
	s := testServer()
	names := listedToolNames(t, s)
	testutil.False(t, names["argus_brief"])
	testutil.True(t, names["kb_delete"])
	testutil.True(t, names["kb_ingest"])

	resp := doRequest(t, s, "tools/call", ToolCallParams{
		Name:      "argus_command",
		Arguments: json.RawMessage(`{"command":"kb_delete"}`),
	})
	text := toolText(t, resp)
	testutil.Equal(t, text, "Voice control is not available.")
	testutil.True(t, toolIsError(t, resp))

	s.SetVoice(voice.NewRouter(mcpVoiceOp{}), "test-token")
	names = listedToolNames(t, s)
	testutil.True(t, names["argus_brief"])
	testutil.True(t, names["argus_command"])
	testutil.True(t, names["argus_commands"])
	testutil.True(t, names["kb_delete"])

	denied := doRequest(t, s, "tools/call", ToolCallParams{
		Name:      "argus_command",
		Arguments: json.RawMessage(`{"command":"kb_delete"}`),
	})
	body := toolText(t, denied)
	testutil.Contains(t, body, "That command is not available by voice.")
	testutil.True(t, toolIsError(t, denied))
	testutil.False(t, strings.Contains(body, "kb_ingest"))

	brief := doRequest(t, s, "tools/call", ToolCallParams{
		Name:      "argus_brief",
		Arguments: json.RawMessage(`{}`),
	})
	testutil.False(t, toolIsError(t, brief))
	var parsed voice.Result
	testutil.NoError(t, json.Unmarshal([]byte(toolText(t, brief)), &parsed))
	testutil.Equal(t, parsed.Speech, "Review is running.")
	testutil.Equal(t, parsed.Counts.Running, 1)
}

type countedVoiceOp struct {
	mcpVoiceOp
	findCalls atomic.Int32
}

func (o *countedVoiceOp) Find(context.Context, string, string, string) (voice.FindResult, error) {
	o.findCalls.Add(1)
	return voice.FindResult{Found: true, Ref: voice.TaskRef{ID: "t1", Name: "Review", Live: true}}, nil
}

func TestVoiceMutationAuthorizationAtHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, host, origin, bearer, query string
		allowed                           bool
	}{
		{name: "absent", host: "127.0.0.1:7742"},
		{name: "wrong token", host: "127.0.0.1:7742", bearer: "Bearer wrong"},
		{name: "query token", host: "127.0.0.1:7742", query: "?token=test-token"},
		{name: "wrong origin", host: "127.0.0.1:7742", origin: "https://example.com", bearer: "Bearer test-token"},
		{name: "wrong host", host: "example.com", bearer: "Bearer test-token"},
		{name: "authorized local", host: "127.0.0.1:7742", bearer: "Bearer test-token", allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer()
			op := &countedVoiceOp{}
			s.SetVoice(voice.NewRouter(op), "test-token")
			t.Cleanup(func() { testutil.NoError(t, s.Shutdown(context.Background())) })
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"argus_command","arguments":{"command":" Send ","task_id":"t1","text":"hello"}}}`
			req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/mcp"+tc.query, strings.NewReader(body))
			req.Header.Set("Authorization", tc.bearer)
			req.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			var resp Response
			testutil.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			var got voice.Result
			testutil.NoError(t, json.Unmarshal([]byte(toolText(t, &resp)), &got))
			if tc.allowed {
				testutil.Equal(t, op.findCalls.Load(), int32(1))
				testutil.True(t, got.JobID != "")
			} else {
				testutil.Equal(t, op.findCalls.Load(), int32(0))
				testutil.Equal(t, got.Detail, "unauthorized")
				testutil.Equal(t, got.JobID, "")
			}
		})
	}
}

func TestVoiceWaitDecodingClampsBeforeDurationConversion(t *testing.T) {
	for _, tc := range []struct {
		name, ms string
		want     time.Duration
	}{
		{"within cap", "125", 125 * time.Millisecond},
		{"above cap", "9223372036854775807", 3 * time.Second},
		{"below zero", "-1", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, errText := decodeVoiceCommand(json.RawMessage(`{"command":"wait","timeout_ms":` + tc.ms + `}`))
			testutil.Equal(t, errText, "")
			testutil.Equal(t, req.Timeout, tc.want)
		})
	}
}

func listedToolNames(t *testing.T, s *Server) map[string]bool {
	t.Helper()
	resp := doRequest(t, s, "tools/list", nil)
	raw, err := json.Marshal(resp.Result)
	testutil.NoError(t, err)
	var list ToolsListResult
	testutil.NoError(t, json.Unmarshal(raw, &list))
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	return names
}

func toolText(t *testing.T, resp *Response) string {
	t.Helper()
	raw, err := json.Marshal(resp.Result)
	testutil.NoError(t, err)
	var result ToolCallResult
	testutil.NoError(t, json.Unmarshal(raw, &result))
	testutil.True(t, len(result.Content) > 0)
	return result.Content[0].Text
}

func toolIsError(t *testing.T, resp *Response) bool {
	t.Helper()
	raw, err := json.Marshal(resp.Result)
	testutil.NoError(t, err)
	var result ToolCallResult
	testutil.NoError(t, json.Unmarshal(raw, &result))
	return result.IsError
}
