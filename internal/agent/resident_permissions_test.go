package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func settingsArg(t *testing.T, args []string) (map[string]any, bool) {
	t.Helper()
	_, rest, ok := strings.Cut(strings.Join(args, " "), "--settings '")
	if !ok {
		return nil, false
	}
	raw, _, _ := strings.Cut(rest, "'")
	var out map[string]any
	testutil.NoError(t, json.Unmarshal([]byte(raw), &out))
	return out, true
}

func residentCfg() config.Config {
	cfg := testConfig()
	cfg.Resident = config.DefaultResidentConfig(100000)
	return cfg
}

func TestBuildCmd_ResidentGetsAskAndDenyRules(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(task, residentCfg(), false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	st, ok := settingsArg(t, cmd.Args)
	if !ok {
		t.Fatalf("resident command has no --settings: %v", cmd.Args)
	}
	perms, _ := st["permissions"].(map[string]any)
	toStrings := func(v any) []string {
		var out []string
		for _, x := range v.([]any) {
			out = append(out, x.(string))
		}
		return out
	}
	allow, ask, deny := toStrings(perms["allow"]), toStrings(perms["ask"]), toStrings(perms["deny"])
	// Read-only tier runs unprompted; KB and recycle must never prompt.
	for _, want := range []string{"mcp__argus__kb_search", "mcp__argus__kb_ingest", "mcp__argus__task_recycle", "mcp__claude_ai_Slack__slack_send_message_draft"} {
		testutil.Contains(t, strings.Join(allow, ","), want)
	}
	testutil.Contains(t, strings.Join(ask, ","), "mcp__claude_ai_Google_Calendar__create_event")
	testutil.Contains(t, strings.Join(deny, ","), "mcp__claude_ai_Slack__slack_send_message")
	for _, d := range deny {
		if strings.HasSuffix(d, "slack_send_message_draft") {
			t.Fatalf("draft tool must not be denied: %v", deny)
		}
	}
}

func TestBuildCmd_ResidentRulesHonorConfigLists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Resident.AllowTools = []string{"mcp__x__allow"}
	cfg.Resident.AskTools = []string{"mcp__x__ask"}
	cfg.Resident.DenyTools = []string{"mcp__x__deny"}
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	st, _ := settingsArg(t, cmd.Args)
	perms := st["permissions"].(map[string]any)
	testutil.DeepEqual(t, perms["allow"], any([]any{"mcp__x__allow"}))
	testutil.DeepEqual(t, perms["ask"], any([]any{"mcp__x__ask"}))
	testutil.DeepEqual(t, perms["deny"], any([]any{"mcp__x__deny"}))
}

func TestBuildCmd_NonResidentGetsNoResidentRules(t *testing.T) {
	cfg := residentCfg()
	task := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if _, ok := settingsArg(t, cmd.Args); ok {
		t.Fatalf("plain task must not receive resident permission rules: %v", cmd.Args)
	}
}

func TestBuildCmd_ResidentEmptyListsInjectNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Resident.AllowTools, cfg.Resident.AskTools, cfg.Resident.DenyTools = nil, nil, nil
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if _, ok := settingsArg(t, cmd.Args); ok {
		t.Fatalf("empty lists should inject no --settings: %v", cmd.Args)
	}
}

// Ask rules only prompt outside bypass modes, so the resident must never run
// under one, whatever the backend command or defaults.permission_mode say.
func TestBuildCmd_ResidentForcesNonBypassPermissionMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Defaults.PermissionMode = config.PermissionModeBypassActive
	cfg.Backends["claude"] = config.Backend{Command: "claude --dangerously-skip-permissions --allow-dangerously-skip-permissions --permission-mode plan --model x"}
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	line := strings.Join(cmd.Args, " ")
	if strings.Contains(line, "skip-permissions") {
		t.Fatalf("bypass flag survived for resident: %s", line)
	}
	testutil.Equal(t, strings.Count(line, "--permission-mode"), 1)
	testutil.Contains(t, line, "--permission-mode default")
	testutil.Contains(t, line, "--model x")
}

func TestBuildCmd_NonResidentKeepsConfiguredPermissionFlags(t *testing.T) {
	cfg := residentCfg()
	task := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	testutil.Contains(t, strings.Join(cmd.Args, " "), "--dangerously-skip-permissions")
}

func TestStripClaudePermissionFlags(t *testing.T) {
	testutil.Equal(t, stripClaudePermissionFlags("claude --dangerously-skip-permissions --permission-mode plan -x"), "claude -x")
	testutil.Equal(t, stripClaudePermissionFlags("claude --permission-mode=acceptEdits"), "claude")
	testutil.Equal(t, stripClaudePermissionFlags("claude --allow-dangerously-skip-permissions"), "claude")
	testutil.Equal(t, stripClaudePermissionFlags("claude --model opus"), "claude --model opus")
}

// The resident's Argus MCP connection is bound to its own task at spawn, which
// is what lets task_recycle sit in the allow tier; other tasks carry no binding.
func TestBuildCmd_ResidentMCPURLBoundToItself(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.MCPPort = 7742
	build := func(task *model.Task) string {
		cmd, cleanup, err := BuildCmd(task, cfg, false)
		testutil.NoError(t, err)
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
		return strings.Join(cmd.Args, " ")
	}
	res := build(&model.Task{ID: "res-9", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()})
	testutil.Contains(t, res, "http://localhost:7742/mcp?caller=res-9")
	plain := build(&model.Task{ID: "p1", Name: "plain", Worktree: t.TempDir()})
	testutil.Contains(t, plain, "http://localhost:7742/mcp")
	if strings.Contains(plain, "caller=") {
		t.Fatalf("plain task must not be caller-bound: %s", plain)
	}
}

func TestBuildCmd_ResidentFailsClosedOnUnenforceableFamily(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := testConfig()
	cfg.Backends["nocodexperm"] = config.Backend{Command: "codex"}
	task := &model.Task{ID: "r1", Kind: model.KindResident, Backend: "nocodexperm", Worktree: db.ResidentWorkDir()}
	_, _, err := BuildCmd(task, cfg, false)
	testutil.Error(t, err)
}
