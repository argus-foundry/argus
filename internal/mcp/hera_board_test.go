package mcp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestHeraBoardTools(t *testing.T) {
	s, d := testHeraServer(t)
	task := seedCoordinator(t, s, d, "board", "/wt/board")
	b, err := d.HeraLiveBindingByTask(task.ID)
	testutil.NoError(t, err)
	call := func(name string, args string) ToolCallResult {
		t.Helper()
		return callResult(t, doRequest(t, s, "tools/call", ToolCallParams{Name: name, Arguments: json.RawMessage(args)}))
	}
	post := call("hera_board_post", `{"cwd":"/wt/board","kind":"decision","subject":"Choose rollout","body":"Context"}`)
	testutil.Equal(t, post.IsError, false)
	items, err := d.ListHeraBoardItems(b.RoleID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	id := items[0].ID
	testutil.NoError(t, d.ReadHeraBoardItem(b.RoleID, id))
	update := call("hera_board_update", fmt.Sprintf(`{"cwd":"/wt/board","id":%d,"kind":"decision","subject":"Choose rollout","body":"Updated"}`, id))
	testutil.Equal(t, update.IsError, false)
	list := call("hera_board_list", `{"cwd":"/wt/board"}`)
	testutil.Equal(t, list.IsError, false)
	testutil.Contains(t, list.Content[0].Text, "Updated")
	for _, tool := range []string{"hera_join", "hera_status"} {
		t.Run(tool, func(t *testing.T) {
			got := call(tool, `{"cwd":"/wt/board","status":"working"}`)
			testutil.Contains(t, got.Content[0].Text, fmt.Sprintf("#%d", id))
			testutil.Contains(t, got.Content[0].Text, "Choose rollout")
		})
	}
	worker := addHeraTestTask(t, d, "/wt/worker")
	role, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: b.OrchestratorID, Name: "worker", Kind: db.HeraKindWorker}, worker.ID, worker.Worktree)
	testutil.NoError(t, err)
	testutil.Equal(t, role.Kind, db.HeraKindWorker)
	for _, verb := range []string{"post", "update", "resolve", "list"} {
		t.Run("worker_"+verb, func(t *testing.T) {
			got := call("hera_board_"+verb, fmt.Sprintf(`{"cwd":"/wt/worker","id":%d,"kind":"fyi","subject":"bad","body":"x"}`, id))
			testutil.Equal(t, got.IsError, true)
			testutil.Contains(t, got.Content[0].Text, "coordinator binding")
		})
	}
	resolved := call("hera_board_resolve", fmt.Sprintf(`{"cwd":"/wt/board","id":%d,"note":"Settled"}`, id))
	testutil.Equal(t, resolved.IsError, false)
	all, err := d.ListHeraBoardItems(b.RoleID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, all[0].ResolvedBy, "agent")
	testutil.Equal(t, all[0].ResolutionNote, "Settled")
}

// Embedding only HeraStore deliberately hides the optional board capability.
type boardUnavailableStore struct{ HeraStore }

func TestHeraBoardToolErrors(t *testing.T) {
	s, d := testHeraServer(t)
	seedCoordinator(t, s, d, "board", "/wt/board")
	for _, tc := range []struct{ name, args, want string }{
		{"invalid JSON", `{`, "invalid board arguments"},
		{"missing cwd", `{}`, "cwd is required"},
		{"unbound", `{"cwd":"/wt/missing"}`, "no task matches cwd"},
		{"invalid kind", `{"cwd":"/wt/board","kind":"bad","subject":"s","body":"b"}`, "invalid item"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := callResult(t, s.toolHeraBoard(1, json.RawMessage(tc.args), "post"))
			testutil.Equal(t, got.IsError, true)
			testutil.Contains(t, got.Content[0].Text, tc.want)
		})
	}
	original := s.heraStore
	s.heraStore = boardUnavailableStore{original}
	got := callResult(t, s.toolHeraBoard(1, json.RawMessage(`{"cwd":"/wt/board"}`), "list"))
	testutil.Equal(t, got.IsError, true)
	testutil.Contains(t, got.Content[0].Text, "board store unavailable")
	s.heraStore = nil
	s.heraSvc = nil
	got = callResult(t, s.toolHeraBoard(1, json.RawMessage(`{"cwd":"/wt/board"}`), "list"))
	testutil.Equal(t, got.IsError, true)
	testutil.Contains(t, got.Content[0].Text, "hera not configured")
}

func TestHeraBoardDismissalInboxSender(t *testing.T) {
	s, d := testHeraServer(t)
	task := seedCoordinator(t, s, d, "board", "/wt/board")
	b, err := d.HeraLiveBindingByTask(task.ID)
	testutil.NoError(t, err)
	i, err := d.PostHeraBoardItem(b.RoleID, "decision", "Choose", "body")
	testutil.NoError(t, err)
	testutil.NoError(t, d.ResolveHeraBoardItem(b.RoleID, i.ID, "human", ""))
	got := callResult(t, doRequest(t, s, "tools/call", ToolCallParams{Name: "hera_inbox", Arguments: json.RawMessage(`{"cwd":"/wt/board"}`)}))
	testutil.False(t, got.IsError)
	testutil.Contains(t, got.Content[0].Text, "from: Aaron (board)")
	testutil.False(t, strings.Contains(got.Content[0].Text, "from: coord"))
}

func TestHeraBoardStructuredTools(t *testing.T) {
	s, d := testHeraServer(t)
	task := seedCoordinator(t, s, d, "structured", "/wt/structured")
	b, err := d.HeraLiveBindingByTask(task.ID)
	testutil.NoError(t, err)
	call := func(verb, args string) ToolCallResult {
		t.Helper()
		return callResult(t, s.toolHeraBoard(1, json.RawMessage(args), verb))
	}
	result := call("post", `{"cwd":"/wt/structured","kind":"decision","subject":"Choose","body":"Situation","options":[{"id":"retry","label":"Retry"},{"id":"wait","label":"Wait"}],"recommended":"retry","reason":"Faster"}`)
	testutil.Equal(t, result.IsError, false)
	items, err := d.ListHeraBoardItems(b.RoleID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 1)
	testutil.Equal(t, items[0].Structured.Recommended, "retry")
	result = call("update", fmt.Sprintf(`{"cwd":"/wt/structured","id":%d,"kind":"decision","subject":"Choose","body":"Situation","recommended":"wait","reason":"Safer"}`, items[0].ID))
	testutil.Equal(t, result.IsError, false)
	result = call("update", fmt.Sprintf(`{"cwd":"/wt/structured","id":%d,"kind":"decision","subject":"Choose","body":"Situation","recommended":"missing"}`, items[0].ID))
	testutil.Equal(t, result.IsError, true)
	for _, tc := range []struct {
		name, fields string
		plain        bool
	}{{"omit", "", false}, {"clear", `,"options":[]`, true}} {
		t.Run(tc.name, func(t *testing.T) {
			result := call("update", fmt.Sprintf(`{"cwd":"/wt/structured","id":%d,"kind":"decision","subject":"Choose","body":"Updated"%s}`, items[0].ID, tc.fields))
			testutil.Equal(t, result.IsError, false)
			current, err := d.ListHeraBoardItems(b.RoleID, false)
			testutil.NoError(t, err)
			testutil.Equal(t, current[0].Structured == nil, tc.plain)
		})
	}
}

func TestBoardToolSchemaMatchesDecode(t *testing.T) {
	fields := map[string]bool{}
	typ := reflect.TypeFor[boardToolParams]()
	for n := range typ.NumField() {
		fields[typ.Field(n).Tag.Get("json")] = true
	}
	for _, tool := range boardToolDefs() {
		t.Run(tool.Name, func(t *testing.T) {
			properties := tool.InputSchema.(map[string]interface{})["properties"].(map[string]interface{})
			testutil.Equal(t, len(properties), len(fields))
			for name := range properties {
				testutil.Equal(t, fields[name], true)
			}
		})
	}
}
