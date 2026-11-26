package mcp

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardWorkTools(t *testing.T) {
	s, d := testHeraServer(t)
	task := seedCoordinator(t, s, d, "work", "/wt/work")
	b, err := d.HeraLiveBindingByTask(task.ID)
	testutil.NoError(t, err)
	call := func(verb, body string) ToolCallResult {
		t.Helper()
		return callResult(t, s.toolHeraBoard(1, json.RawMessage(body), verb))
	}
	for _, tc := range []struct {
		name, body string
		invalid    bool
	}{
		{"blocking action", `{"cwd":"/wt/work","kind":"action","subject":"Unlock","body":"Step","blocking":true,"command":"Unlock 1Password"}`, false},
		{"pending todo", `{"cwd":"/wt/work","kind":"todo","subject":"Later","body":"Team work","trigger":"after 1.42"}`, false},
		{"missing blocking", `{"cwd":"/wt/work","kind":"action","subject":"Step","body":"Step"}`, true},
		{"todo command", `{"cwd":"/wt/work","kind":"todo","subject":"Step","body":"Step","command":"run"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) { testutil.Equal(t, call("post", tc.body).IsError, tc.invalid) })
	}
	items, err := d.ListHeraBoardItems(b.RoleID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(items), 2)
	testutil.Equal(t, items[0].Command, "Unlock 1Password")
	testutil.Equal(t, items[1].TriggerPending, true)
	list := call("list", `{"cwd":"/wt/work","kind":"todo"}`)
	testutil.Equal(t, list.IsError, false)
	var filtered []struct{ Kind string }
	testutil.NoError(t, json.Unmarshal([]byte(list.Content[0].Text), &filtered))
	testutil.Equal(t, len(filtered), 1)
	testutil.Equal(t, filtered[0].Kind, "todo")
	testutil.Equal(t, call("list", `{"cwd":"/wt/work","kind":"bad"}`).IsError, true)
	update := call("update", fmt.Sprintf(`{"cwd":"/wt/work","id":%d,"kind":"todo","subject":"Later","body":"Team work","trigger_pending":false}`, items[1].ID))
	testutil.Equal(t, update.IsError, false)
	testutil.Equal(t, call("resolve", fmt.Sprintf(`{"cwd":"/wt/work","id":%d,"outcome":"dropped"}`, items[0].ID)).IsError, true)
	testutil.Equal(t, call("resolve", fmt.Sprintf(`{"cwd":"/wt/work","id":%d}`, items[0].ID)).IsError, false)
	testutil.Equal(t, call("resolve", fmt.Sprintf(`{"cwd":"/wt/work","id":%d,"outcome":"dropped"}`, items[1].ID)).IsError, false)
	all, err := d.ListHeraBoardItems(b.RoleID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, all[0].ResolutionChoice, "done")
	testutil.Equal(t, all[1].ResolutionChoice, "dropped")
}
