package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

const nonCanonicalRefusal = "argument names must be canonical lower-case"

// The canonical-key guard refuses every key outside ^[a-z0-9_]+$, so no
// built-in schema may declare one.
func TestBuiltinToolSchemasUseCanonicalKeys(t *testing.T) {
	defs := builtinToolDefs()
	testutil.True(t, len(defs) > 50)
	for _, tool := range defs {
		schema, ok := tool.InputSchema.(map[string]interface{})
		if !ok {
			t.Fatalf("%s: schema is %T", tool.Name, tool.InputSchema)
		}
		props, _ := schema["properties"].(map[string]interface{})
		for k := range props {
			if !canonicalArgKey(k) {
				t.Errorf("%s declares non-canonical argument %q", tool.Name, k)
			}
		}
		testutil.True(t, builtinToolNames()[tool.Name])
	}
}

func TestCanonicalArgKey(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want bool
	}{
		{"cwd", true}, {"caller_id", true}, {"role_name", true}, {"v2", true},
		{"", false}, {"CWD", false}, {"Cwd", false}, {"cᴡd", false}, {"orcheſtrator", false},
		{"tasK_id", false}, {"role-name", false}, {"cwd ", false}, {"roleName", false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			testutil.Equal(t, canonicalArgKey(tc.key), tc.want)
		})
	}
}

// The resident identity guard reads exact lower-case keys while every tool's
// encoding/json decode folds case (U+017F 'ſ'→s, U+212A Kelvin→k too). Each
// of these reached its tool before the canonical-key guard.
func TestCanonicalArgKeys_HeraUnboundRefused(t *testing.T) {
	for _, tc := range []struct {
		name, tool string
		args       func(res *model.Task) map[string]interface{}
	}{
		{"spawn CWD", "hera_spawn_worker", func(res *model.Task) map[string]interface{} {
			return map[string]interface{}{"CWD": res.Worktree, "prompt": "do it", "role_name": "rogue", "project": "test-project"}
		}},
		{"spawn Kelvin key", "hera_spawn_worker", func(res *model.Task) map[string]interface{} {
			return map[string]interface{}{"Cwd": res.Worktree, "prompt": "do it", "role_name": "rogue", "project": "test-project", "bacKend": "claude"}
		}},
		{"status Cwd", "hera_status", func(res *model.Task) map[string]interface{} {
			return map[string]interface{}{"Cwd": res.Worktree, "status": "working", "handoff_note": "forged", "request_recycle": true}
		}},
		{"status long-s fold", "hera_status", func(res *model.Task) map[string]interface{} {
			return map[string]interface{}{"cwd": "/elsewhere", "ſtatus": "working", "handoff_note": "forged", "orcheſtrator": db.ResidentOrchestratorName}
		}},
		{"send small-capital w", "hera_send", func(res *model.Task) map[string]interface{} {
			return map[string]interface{}{"cᴡd": res.Worktree, "to": "worker", "tldr": "order", "body": "forged order"}
		}},
		{"send CWD", "hera_send", func(res *model.Task) map[string]interface{} {
			return map[string]interface{}{"CWD": res.Worktree, "to": "worker", "tldr": "order", "body": "forged order"}
		}},
		{"join Orchestrator", "hera_join", func(*model.Task) map[string]interface{} {
			return map[string]interface{}{"cwd": "/wt/outsider", "Orchestrator": db.ResidentOrchestratorName, "role_name": "x", "kind": "worker"}
		}},
		{"join long-s orchestrator", "hera_join", func(*model.Task) map[string]interface{} {
			return map[string]interface{}{"cwd": "/wt/outsider", "orcheſtrator": db.ResidentOrchestratorName, "role_name": "x", "kind": "worker"}
		}},
		{"join Role_Name", "hera_join", func(*model.Task) map[string]interface{} {
			return map[string]interface{}{"cwd": "/wt/outsider", "orchestrator": db.ResidentOrchestratorName, "Role_Name": "x", "kind": "worker"}
		}},
		{"rebind Orchestrator", "hera_rebind", func(*model.Task) map[string]interface{} {
			return map[string]interface{}{"cwd": "/wt/outsider", "Orchestrator": db.ResidentOrchestratorName, "role_name": "x"}
		}},
	} {
		for _, conn := range []string{"unbound", "forged caller"} {
			t.Run(tc.name+"/"+conn, func(t *testing.T) {
				s, d, res, o := residentHeraFixture(t)
				testutil.NoError(t, d.SetProject("test-project", config.Project{Path: t.TempDir()}))
				worker, _, _ := residentTestWorker(t, d, o.ID, "worker")
				outsider := addHeraTestTask(t, d, "/wt/outsider")
				path := "/mcp"
				if conn == "forged caller" {
					path = "/mcp?caller=" + res.ID // resident id, no token
				}
				before, err := d.Tasks()
				testutil.NoError(t, err)
				raw, err := json.Marshal(tc.args(res))
				testutil.NoError(t, err)
				cr := callResult(t, postRaw(t, s, path, "", "tools/call", ToolCallParams{Name: tc.tool, Arguments: raw}))
				testutil.True(t, cr.IsError)
				testutil.Contains(t, cr.Content[0].Text, "invalid argument key")
				testutil.Contains(t, cr.Content[0].Text, nonCanonicalRefusal)

				after, err := d.Tasks()
				testutil.NoError(t, err)
				testutil.Equal(t, len(after), len(before))
				for _, name := range []string{"rogue", "x"} {
					_, err = d.HeraRoleByName(o.ID, name)
					testutil.ErrorIs(t, err, db.ErrHeraNotFound)
				}
				testutil.False(t, hasRecycleMeta(t, d, res.ID))
				for _, id := range []string{worker.ID, res.ID} {
					msgs, err := d.HeraMessagesForTask(id, 50)
					testutil.NoError(t, err)
					testutil.Equal(t, len(msgs), 0)
				}
				_, err = d.HeraLiveBindingByTask(outsider.ID)
				testutil.ErrorIs(t, err, db.ErrHeraNotFound)
			})
		}
	}
}

// The verified resident's Hera arguments are pinned to its own worktree and
// team; a folded duplicate key must not ride past that pin.
func TestCanonicalArgKeys_BoundResidentCannotOverridePin(t *testing.T) {
	for _, key := range []string{"CWD", "Cwd", "orcheſtrator", "Orchestrator"} {
		t.Run(key, func(t *testing.T) {
			s, d, res, o := residentHeraFixture(t)
			worker, _, _ := residentTestWorker(t, d, o.ID, "worker")
			other, err := d.CreateHeraOrchestrator("other", "")
			testutil.NoError(t, err)
			coord := addHeraTestTask(t, d, "/wt/other-coord")
			_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: other.ID, Name: "coord", Kind: db.HeraKindCoordinator}, coord.ID, coord.Worktree)
			testutil.NoError(t, err)
			value := coord.Worktree
			if key != "CWD" && key != "Cwd" {
				value = "other"
			}
			raw, err := json.Marshal(map[string]interface{}{key: value, "to": "worker", "tldr": "order", "body": "x"})
			testutil.NoError(t, err)
			text, isErr := postTool(t, s, "/mcp?caller="+res.ID, "hera_send", string(raw))
			testutil.True(t, isErr)
			testutil.Contains(t, text, nonCanonicalRefusal)
			msgs, err := d.HeraMessagesForTask(worker.ID, 50)
			testutil.NoError(t, err)
			testutil.Equal(t, len(msgs), 0)

			// The canonical spelling still works and acts as the resident.
			_, isErr = postTool(t, s, "/mcp?caller="+res.ID, "hera_send", `{"cwd":"/elsewhere","to":"worker","tldr":"order","body":"real"}`)
			testutil.False(t, isErr)
			msgs, err = d.HeraMessagesForTask(worker.ID, 50)
			testutil.NoError(t, err)
			testutil.Equal(t, len(msgs), 1)
		})
	}
}

func TestCanonicalArgKeys_TaskToolsRefused(t *testing.T) {
	for _, tc := range []struct{ name, tool, args string }{
		{"create Caller_ID", "task_create", `{"Caller_ID":"res-1","prompt":"x","project":"myapp"}`},
		{"create CALLER_ID", "task_create", `{"CALLER_ID":"res-1","prompt":"x","project":"myapp"}`},
		{"inbox ID", "task_inbox", `{"ID":"res-1"}`},
		{"inbox Id", "task_inbox", `{"Id":"res-1"}`},
		{"send ID", "task_message_send", `{"ID":"res-1","to":"abc123","body":"forged"}`},
		{"set result ID", "task_set_result", `{"ID":"res-1","result":{"ok":true}}`},
		{"inbox CWD", "task_inbox", `{"CWD":"/home/x/.argus/resident"}`},
	} {
		for _, conn := range []string{"unbound", "forged caller"} {
			t.Run(tc.name+"/"+conn, func(t *testing.T) {
				s, rdb := residentServer(t)
				store := &countingStore{mockMessageStore: &mockMessageStore{}}
				s.SetMessageManager(store, &mockNudger{})
				created := 0
				s.createTask = func(TaskCreateInput) (*model.Task, error) { created++; return &model.Task{ID: "new"}, nil }
				path := "/mcp"
				if conn == "forged caller" {
					path = "/mcp?caller=res-1"
				}
				cr := callResult(t, postRaw(t, s, path, "", "tools/call", ToolCallParams{Name: tc.tool, Arguments: json.RawMessage(tc.args)}))
				testutil.True(t, cr.IsError)
				testutil.Contains(t, cr.Content[0].Text, nonCanonicalRefusal)
				testutil.Equal(t, store.inboxReads, 0)
				testutil.Equal(t, len(store.messages), 0)
				testutil.Equal(t, rdb.resident.Result, "")
				testutil.Equal(t, created, 0)
			})
		}
	}
	t.Run("lowercase still works", func(t *testing.T) {
		s, _ := residentServer(t)
		store := &countingStore{mockMessageStore: &mockMessageStore{}}
		s.SetMessageManager(store, &mockNudger{})
		_, isErr := postTool(t, s, "/mcp", "task_inbox", `{"id":"abc123"}`)
		testutil.False(t, isErr)
		testutil.Equal(t, store.inboxReads, 1)
	})
}

// Plugin tools own their schemas (which may be camelCase) and get no Argus
// identity, so only fold-colliding keys are refused for them.
func TestCanonicalArgKeys_PluginTools(t *testing.T) {
	calls := 0
	plugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"isError":false}`))
	}))
	t.Cleanup(plugin.Close)
	for _, tc := range []struct {
		name, args string
		refused    bool
	}{
		{"camelCase", `{"sessionId":"a","Text":"b"}`, false},
		{"fold collision", `{"text":"a","TEXT":"b"}`, true},
		{"unicode fold collision", `{"task":"a","tasK":"b"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, reg, _ := newMCPWithRegistry(t)
			testutil.NoError(t, reg.Register("ludwig", ToolRegistration{Name: "ludwig_decision_add", InputSchema: json.RawMessage(`{}`), CallbackURL: plugin.URL}))
			before := calls
			cr := callResult(t, doRequest(t, s, "tools/call", ToolCallParams{Name: "ludwig_decision_add", Arguments: json.RawMessage(tc.args)}))
			testutil.Equal(t, cr.IsError, tc.refused)
			if tc.refused {
				testutil.Contains(t, cr.Content[0].Text, nonCanonicalRefusal)
				testutil.Equal(t, calls, before)
			} else {
				testutil.Equal(t, calls, before+1)
			}
		})
	}
}

// Every tool that names a task by "id", "task_id" or "caller_id" refuses the
// resident's id from any connection but the verified resident: before the
// inverted check, task_stop (and anything else outside the old self-id
// allowlist) acted on the resident.
func TestResidentTaskIDGuard_EveryTool(t *testing.T) {
	tools := []string{
		"task_get", "task_stop", "task_archive", "task_rename", "task_complete", "task_set_result", "task_recycle",
		"argus_clipboard_set", "sandbox_propose_write_paths", "task_message_send", "task_inbox", "task_message_ack",
		"task_ask", "artifact_register", "artifact_publish", "artifact_open", "task_move_account", "task_create",
	}
	// A literal list, not the production residentTaskIDArgs slice: dropping a
	// key there must fail this test rather than silently shrink it.
	for _, key := range []string{"id", "task_id", "caller_id"} {
		for _, name := range tools {
			for _, conn := range []string{"unbound", "forged caller", "other task"} {
				t.Run(key+"/"+name+"/"+conn, func(t *testing.T) {
					s, rdb := residentServer(t)
					store := &countingStore{mockMessageStore: &mockMessageStore{}}
					s.SetMessageManager(store, &mockNudger{})
					created := 0
					s.createTask = func(TaskCreateInput) (*model.Task, error) { created++; return &model.Task{ID: "new"}, nil }
					path, token := "/mcp", ""
					switch conn {
					case "forged caller":
						path = "/mcp?caller=res-1" // resident id, no token
					case "other task":
						path, token = "/mcp?caller=abc123", armedToken(t, s) //nolint:gosec // G101: test fixture token minted by the test
					}
					args := map[string]interface{}{key: "res-1", "name": "renamed", "to": "abc123", "body": "x", "result": map[string]bool{"ok": true},
						"message_ids": []string{"m1"}, "question": "q", "text": "x", "prompt": "x", "project": "myapp", "account": "other", "handoff_note": "x"}
					raw, err := json.Marshal(args)
					testutil.NoError(t, err)
					cr := callResult(t, postRaw(t, s, path, token, "tools/call", ToolCallParams{Name: name, Arguments: raw}))
					testutil.True(t, cr.IsError)
					testutil.Contains(t, cr.Content[0].Text, name+": resident identity requires the resident's own bound connection")
					testutil.Equal(t, len(s.taskStopper.(*mockStopper).stopped), 0)
					testutil.Equal(t, rdb.resident.Status, model.StatusInProgress)
					testutil.False(t, rdb.resident.Archived)
					testutil.Equal(t, rdb.resident.Name, "resident")
					testutil.Equal(t, rdb.resident.Result, "")
					testutil.Equal(t, store.inboxReads, 0)
					testutil.Equal(t, len(store.messages), 0)
					testutil.Equal(t, created, 0)
				})
			}
		}
	}
}

func TestResidentTaskIDGuard_LegitimateCallsStillWork(t *testing.T) {
	t.Run("ordinary task id", func(t *testing.T) {
		s, _ := residentServer(t)
		_, isErr := postTool(t, s, "/mcp", "task_stop", `{"id":"abc123"}`)
		testutil.False(t, isErr)
		testutil.DeepEqual(t, s.taskStopper.(*mockStopper).stopped, []string{"abc123"})
	})
	t.Run("verified resident names itself", func(t *testing.T) {
		s, rdb := residentServer(t)
		_, isErr := postTool(t, s, "/mcp?caller=res-1", "task_set_result", `{"id":"res-1","result":{"ok":true}}`)
		testutil.False(t, isErr)
		testutil.Contains(t, rdb.resident.Result, "ok")
	})
	t.Run("hera board integer id", func(t *testing.T) {
		// Board ids are integers: a number never matches the resident's task id.
		s, _ := residentServer(t)
		text, _ := postTool(t, s, "/mcp", "hera_board_update", `{"id":1,"kind":"decision","subject":"s","body":"b"}`)
		if strings.Contains(text, "resident identity") {
			t.Fatalf("integer board id tripped the resident guard: %s", text)
		}
	})
}

const residentIdentityRefusal = "resident identity requires the resident's own bound connection"

// task_move_account names its target by task_id: pinned independently of the
// key loop above so the most damaging target stays covered.
func TestResidentTaskIDGuard_TaskMoveAccountTaskID(t *testing.T) {
	s, _ := residentServer(t)
	moved := 0
	s.SetAccountMover(func(agent.AccountMoveInput) (agent.AccountMoveResult, error) {
		moved++
		return agent.AccountMoveResult{}, nil
	})
	text, isErr := postTool(t, s, "/mcp", "task_move_account", `{"task_id":"res-1","account":"other"}`)
	testutil.True(t, isErr)
	testutil.Contains(t, text, "task_move_account: "+residentIdentityRefusal)
	testutil.Equal(t, moved, 0)
}

// Built-in receivers trim id and cwd values (task_move_account's
// task_id/caller_id, profile_resolve's cwd), so the guard compares trimmed,
// cleaned values too: a padded resident identity is refused before dispatch.
func TestResidentIdentityGuard_BuiltinValueNormalization(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	for _, tc := range []struct{ name, tool, args string }{
		{"move padded task_id", "task_move_account", `{"task_id":"res-1 ","account":"other"}`},
		{"move tab task_id", "task_move_account", `{"task_id":"\tres-1","account":"other"}`},
		{"move padded caller_id", "task_move_account", `{"task_id":"abc123","caller_id":" res-1","account":"other"}`},
		{"move upper task_id", "task_move_account", `{"task_id":"RES-1","account":"other"}`},
		{"profile padded cwd", "profile_resolve", `{"cwd":" /home/x/.argus/resident"}`},
		{"profile trailing slash cwd", "profile_resolve", `{"cwd":"/home/x/.argus/resident/ "}`},
		{"profile tilde cwd", "profile_resolve", `{"cwd":"~/.argus/resident"}`},
		{"profile dotted cwd", "profile_resolve", `{"cwd":"/home/x/.argus/./resident/sub/.."}`},
		{"stop padded id", "task_stop", `{"id":" res-1 "}`},
		{"inbox case-folded cwd", "task_inbox", `{"cwd":"/HOME/X/.argus/Resident/sub"}`},
	} {
		for _, conn := range []string{"unbound", "forged caller"} {
			t.Run(tc.name+"/"+conn, func(t *testing.T) {
				s, rdb := residentServer(t)
				store := &countingStore{mockMessageStore: &mockMessageStore{}}
				s.SetMessageManager(store, &mockNudger{})
				moved := 0
				s.SetAccountMover(func(agent.AccountMoveInput) (agent.AccountMoveResult, error) {
					moved++
					return agent.AccountMoveResult{}, nil
				})
				path := "/mcp"
				if conn == "forged caller" {
					path = "/mcp?caller=res-1"
				}
				cr := callResult(t, postRaw(t, s, path, "", "tools/call", ToolCallParams{Name: tc.tool, Arguments: json.RawMessage(tc.args)}))
				testutil.True(t, cr.IsError)
				testutil.Contains(t, cr.Content[0].Text, tc.tool+": "+residentIdentityRefusal)
				testutil.Equal(t, moved, 0)
				testutil.Equal(t, store.inboxReads, 0)
				testutil.Equal(t, len(s.taskStopper.(*mockStopper).stopped), 0)
				testutil.Equal(t, rdb.resident.Status, model.StatusInProgress)
			})
		}
	}
}

// Plugin tools keep their own schemas and may decode keys
// case-insensitively (iris reads task_id, plannotator cwd), so the identity
// guard folds keys for them: a folded or padded resident identity never
// reaches the plugin from a non-resident connection.
func TestResidentIdentityGuard_PluginTools(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	calls := 0
	plugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"isError":false}`))
	}))
	t.Cleanup(plugin.Close)
	newSrv := func(t *testing.T) *Server {
		s, _ := residentServer(t)
		d, err := db.OpenInMemory()
		testutil.NoError(t, err)
		t.Cleanup(func() { _ = d.Close() })
		reg := NewRegistry(d)
		s.SetPluginRegistry(reg)
		testutil.NoError(t, reg.Register("iris", ToolRegistration{Name: "iris_push", InputSchema: json.RawMessage(`{}`), CallbackURL: plugin.URL}))
		testutil.NoError(t, reg.Register("plannotator", ToolRegistration{Name: "plannotator_annotate", InputSchema: json.RawMessage(`{}`), CallbackURL: plugin.URL}))
		return s
	}
	for _, tc := range []struct{ name, tool, args string }{
		{"iris Task_ID", "iris_push", `{"Task_ID":"res-1"}`},
		{"iris TASK_ID padded", "iris_push", `{"TASK_ID":" res-1 "}`},
		{"iris exact task_id", "iris_push", `{"task_id":"res-1"}`},
		{"iris ID", "iris_push", `{"ID":"res-1"}`},
		{"iris Caller_Id", "iris_push", `{"Caller_Id":"res-1"}`},
		// U+212A KELVIN SIGN folds to 'k' under Unicode simple folding but is
		// not ASCII: an ASCII-only fold would let it through.
		{"iris Kelvin tasK_id", "iris_push", `{"tas\u212a_id":"res-1"}`},
		{"iris second identity key", "iris_push", `{"id":"abc123","Task_ID":"res-1"}`},
		{"plannotator CWD tilde", "plannotator_annotate", `{"CWD":"~/.argus/resident"}`},
		{"plannotator Cwd padded", "plannotator_annotate", `{"Cwd":" /home/x/.argus/resident/notes "}`},
		// U+017F LONG S folds to 's'. Every identity key (task_id, id,
		// caller_id, ...) lacks an 's' except the cwd value path, so the fold is
		// exercised on the value (and never needs an ASCII-only compare).
		{"plannotator cwd long-s", "plannotator_annotate", `{"cwd":"/home/x/.argus/re\u017fident"}`},
		{"plannotator cwd case-folded", "plannotator_annotate", `{"cwd":"/home/x/.ARGUS/resident"}`},
	} {
		for _, conn := range []string{"unbound", "forged caller"} {
			t.Run(tc.name+"/"+conn, func(t *testing.T) {
				s := newSrv(t)
				path := "/mcp"
				if conn == "forged caller" {
					path = "/mcp?caller=res-1"
				}
				before := calls
				cr := callResult(t, postRaw(t, s, path, "", "tools/call", ToolCallParams{Name: tc.tool, Arguments: json.RawMessage(tc.args)}))
				testutil.True(t, cr.IsError)
				testutil.Contains(t, cr.Content[0].Text, tc.tool+": "+residentIdentityRefusal)
				testutil.Equal(t, calls, before)
			})
		}
	}
	for _, tc := range []struct{ name, path, args string }{
		{"ordinary task id", "/mcp", `{"Task_ID":"abc123","cwd":"/wt/elsewhere"}`},
		{"ordinary camelCase args", "/mcp", `{"sessionId":"s1","Text":"hello"}`},
		{"sibling of resident dir", "/mcp", `{"CWD":"/home/x/.argus/resident-notes"}`},
		{"verified resident names itself", "/mcp?caller=res-1", `{"Task_ID":"res-1","CWD":"/home/x/.argus/resident"}`},
	} {
		t.Run("allowed/"+tc.name, func(t *testing.T) {
			s := newSrv(t)
			before := calls
			text, isErr := postTool(t, s, tc.path, "iris_push", tc.args)
			testutil.False(t, isErr)
			testutil.Equal(t, text, "ok")
			testutil.Equal(t, calls, before+1)
		})
	}
}

// A symlink to the resident's real worktree identifies the resident: the
// symlink-resolved form of the cwd must be compared, not just the literal.
func TestResidentIdentityGuard_PluginCwdSymlink(t *testing.T) {
	real := filepath.Join(t.TempDir(), "resident")
	testutil.NoError(t, os.MkdirAll(filepath.Join(real, "sub"), 0o755))
	link := filepath.Join(t.TempDir(), "alias")
	testutil.NoError(t, os.Symlink(real, link))

	calls := 0
	plugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"isError":false}`))
	}))
	t.Cleanup(plugin.Close)

	for _, cwd := range []string{link, filepath.Join(link, "sub")} {
		t.Run(cwd, func(t *testing.T) {
			s, rdb := residentServer(t)
			rdb.resident.Worktree = real
			d, err := db.OpenInMemory()
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			reg := NewRegistry(d)
			s.SetPluginRegistry(reg)
			testutil.NoError(t, reg.Register("plannotator", ToolRegistration{Name: "plannotator_annotate", InputSchema: json.RawMessage(`{}`), CallbackURL: plugin.URL}))
			args, err := json.Marshal(map[string]string{"cwd": cwd})
			testutil.NoError(t, err)
			before := calls
			cr := callResult(t, postRaw(t, s, "/mcp", "", "tools/call", ToolCallParams{Name: "plannotator_annotate", Arguments: args}))
			testutil.True(t, cr.IsError)
			testutil.Contains(t, cr.Content[0].Text, "plannotator_annotate: "+residentIdentityRefusal)
			testutil.Equal(t, calls, before)
		})
	}
}

func TestPathWithinFold(t *testing.T) {
	for _, tc := range []struct {
		p, dir string
		want   bool
	}{
		{"/home/x/.argus/resident", "/home/x/.argus/resident", true},
		{"/home/x/.argus/resident/a/b", "/home/x/.argus/resident", true},
		{"/HOME/X/.ARGUS/RESIDENT", "/home/x/.argus/resident", true},
		{"/home/x/.argus/residentx", "/home/x/.argus/resident", false},
		{"/home/x/.argus", "/home/x/.argus/resident", false},
		{"/other/resident", "/home/x/.argus/resident", false},
	} {
		t.Run(tc.p, func(t *testing.T) {
			testutil.Equal(t, pathWithinFold(tc.p, tc.dir), tc.want)
		})
	}
}
