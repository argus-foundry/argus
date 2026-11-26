package mcp

import (
	"context"
	"encoding/json"
	"strings"
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

	s.SetVoice(voice.NewRouter(mcpVoiceOp{}))
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
