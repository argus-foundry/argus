package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
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
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir(), Prompt: "hi"}
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
	// The prompt is a positional arg: --settings and the forced permission
	// mode must come before it or the CLI would not apply them.
	line := strings.Join(cmd.Args, " ")
	pi := strings.Index(line, "'hi'")
	si := strings.Index(line, "--settings")
	mi := strings.Index(line, "--permission-mode default")
	if pi < 0 || si < 0 || mi < 0 {
		t.Fatalf("missing prompt/--settings/--permission-mode default in %s", line)
	}
	if si > pi || mi > pi {
		t.Fatalf("--settings (%d) and --permission-mode (%d) must precede the prompt (%d)", si, mi, pi)
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

// Empty lists must still isolate the resident from user settings: nothing is
// then allowed unprompted, rather than the user's allow rules applying.
func TestBuildCmd_ResidentEmptyListsStillIsolated(t *testing.T) {
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
	testutil.Contains(t, strings.Join(cmd.Args, " "), "--setting-sources project")
	st, ok := settingsArg(t, cmd.Args)
	if !ok {
		t.Fatalf("resident must always carry inline --settings: %v", cmd.Args)
	}
	perms, _ := st["permissions"].(map[string]any)
	if _, has := perms["allow"]; has {
		t.Fatalf("empty allow list must stay absent: %v", perms)
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
	tests := []struct {
		name, in, want string
	}{
		{"all forms", "claude --dangerously-skip-permissions --permission-mode plan -x", "claude -x"},
		{"equals form", "claude --permission-mode=acceptEdits", "claude"},
		{"allow bypass", "claude --allow-dangerously-skip-permissions", "claude"},
		{"untouched", "claude --model opus", "claude --model opus"},
		{"quoted arg internal spaces survive", `claude --append-system-prompt "a   b  c" --permission-mode plan --model x`, `claude --append-system-prompt "a   b  c" --model x`},
		{"quoted mention not stripped", `claude --append-system-prompt "use --permission-mode x and --dangerously-skip-permissions"`, `claude --append-system-prompt "use --permission-mode x and --dangerously-skip-permissions"`},
		{"single quotes", `claude --settings '{"a":  "--permission-mode x"}' --dangerously-skip-permissions`, `claude --settings '{"a":  "--permission-mode x"}'`},
		{"escaped quote", `claude --append-system-prompt "say \"hi  there\"" --permission-mode=plan`, `claude --append-system-prompt "say \"hi  there\""`},
		{"backslash space", `claude --add-dir /a\ b --permission-mode plan`, `claude --add-dir /a\ b`},
		{"quoted flag word is the flag", `claude "--permission-mode" x`, `claude`},
		{"newline preserved", "claude --model x\nfoo --permission-mode plan", "claude --model x\nfoo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stripClaudePermissionFlags(tt.in)
			testutil.NoError(t, err)
			testutil.Equal(t, got, tt.want)
		})
	}
}

// shellArgv runs words through a real sh exactly as BuildCmd's `sh -c` would
// and returns the resulting argv, so splice bugs that only matter to the shell
// (dangling continuation, joined escaped space) are caught.
func shellArgv(t *testing.T, words string) []string {
	t.Helper()
	out, err := exec.Command("sh", "-c", "set -- "+words+"; for a in \"$@\"; do printf '[%s]' \"$a\"; done").Output()
	testutil.NoError(t, err)
	s := string(out)
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"), "][")
}

func TestStripClaudePermissionFlags_ShellOracle(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"continuation before dropped flag", "claude \\\n  --dangerously-skip-permissions", []string{"claude"}},
		{"escaped space kept word", "claude --add-dir foo\\  --dangerously-skip-permissions --model x", []string{"claude", "--add-dir", "foo ", "--model", "x"}},
		{"leading dropped flag", "--dangerously-skip-permissions claude --model x", []string{"claude", "--model", "x"}},
		{"leading two dropped", "--dangerously-skip-permissions --permission-mode plan claude", []string{"claude"}},
		{"double-quoted flag", `claude "--dangerously-skip-permissions" --model x`, []string{"claude", "--model", "x"}},
		{"flag then empty quotes", `claude --dangerously-skip-permissions"" --model x`, []string{"claude", "--model", "x"}},
		{"single-quoted allow flag", `claude '--allow-dangerously-skip-permissions' -p`, []string{"claude", "-p"}},
		{"quoted equals form", `claude --permission-mode="bypassPermissions" -p`, []string{"claude", "-p"}},
		{"quoted two-word form", `claude "--permission-mode" plan -p`, []string{"claude", "-p"}},
		{"quoted value of two-word form", `claude --permission-mode 'plan' -p`, []string{"claude", "-p"}},
		{"adjacent quoted segments", `claude --dangerously"-skip"'-permissions' -p`, []string{"claude", "-p"}},
		{"equals form", "claude --permission-mode=x -p", []string{"claude", "-p"}},
		{"flag as last word", "claude -p --dangerously-skip-permissions", []string{"claude", "-p"}},
		{"permission-mode last with no value", "claude -p --permission-mode", []string{"claude", "-p"}},
		{"quoted mention survives", `claude --append-system-prompt "use --permission-mode x" -p`, []string{"claude", "--append-system-prompt", "use --permission-mode x", "-p"}},
		{"continuation inside kept words", "claude \\\n --model x \\\n --permission-mode plan \\\n -p", []string{"claude", "--model", "x", "-p"}},
		{"untouched", "claude --model opus", []string{"claude", "--model", "opus"}},
		{"disallowedTools kept", `claude --disallowedTools "Bash(rm *)" -p`, []string{"claude", "--disallowedTools", "Bash(rm *)", "-p"}},
		{"allowedTools quoted mention survives", `claude --append-system-prompt "--allowedTools x" -p`, []string{"claude", "--append-system-prompt", "--allowedTools x", "-p"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stripClaudePermissionFlags(tt.in)
			testutil.NoError(t, err)
			testutil.DeepEqual(t, shellArgv(t, got), tt.want)
			if strings.HasSuffix(got, "\\") {
				t.Fatalf("result ends in a dangling backslash: %q", got)
			}
		})
	}
}

func TestBuildCmd_ResidentContinuationKeepsForcedModeArgv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Backends["claude"] = config.Backend{Command: "claude \\\n  --dangerously-skip-permissions"}
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	// Evaluate the real sh -c script's words under sh: the forced mode must
	// arrive as two separate argv entries right after the program.
	testutil.Equal(t, cmd.Args[0], "sh")
	argv := shellArgv(t, cmd.Args[len(cmd.Args)-1])
	if len(argv) < 3 {
		t.Fatalf("argv too short: %q", argv)
	}
	testutil.DeepEqual(t, argv[:3], []string{"claude", "--permission-mode", "default"})
}

func TestStripClaudePermissionFlags_UnbalancedQuoteErrors(t *testing.T) {
	for _, in := range []string{`claude --x "oops`, `claude --x 'oops`, `claude --x oops\`} {
		t.Run(in, func(t *testing.T) {
			_, err := stripClaudePermissionFlags(in)
			if err == nil {
				t.Fatalf("expected error for %q", in)
			}
		})
	}
}

func TestBuildCmd_ResidentUnbalancedBackendCommandErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Backends["claude"] = config.Backend{Command: `claude --append-system-prompt "oops --permission-mode bypassPermissions`}
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	if _, _, err := BuildCmd(task, cfg, false); err == nil {
		t.Fatal("expected BuildCmd to refuse an unparseable resident command")
	}
}

// A quoted argument that merely mentions --permission-mode must not suppress
// the forced default mode (HasPermissionFlag is a substring search).
func TestBuildCmd_ResidentRefusesQuotedArgumentCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Backends["claude"] = config.Backend{Command: `claude --append-system-prompt "never use --permission-mode bypassPermissions"`}
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	_, _, err := BuildCmd(task, cfg, false)
	if err == nil {
		t.Fatal("expected the resident to refuse an --append-system-prompt command")
	}
	testutil.Contains(t, err.Error(), "--append-system-prompt")
}

func TestBuildCmd_ResidentMarkerEnvResidentOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	has := func(cmd *exec.Cmd) bool {
		for _, kv := range cmd.Env {
			if kv == ResidentEnvMarker+"=1" {
				return true
			}
		}
		return false
	}
	res := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(res, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	testutil.Equal(t, has(cmd), true)
	plain := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir()}
	cmd2, cleanup2, err := BuildCmd(plain, cfg, false)
	testutil.NoError(t, err)
	if cleanup2 != nil {
		t.Cleanup(cleanup2)
	}
	testutil.Equal(t, has(cmd2), false)
}

// The resident's caller-bound MCP URL now rides only its private config file:
// see TestBuildCmd_ResidentMCPTokenFileOnlyAndRotates and
// TestBuildCmd_PlainTaskKeepsInlineUnboundMCP (resident_mcp_token_test.go).

func TestBuildCmd_ResidentFailsClosedOnUnenforceableFamily(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := testConfig()
	cfg.Backends["nocodexperm"] = config.Backend{Command: "codex"}
	// The resident dir must exist, or BuildCmd fails earlier on the missing
	// worktree and the family check is never reached.
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	task := &model.Task{ID: "r1", Kind: model.KindResident, Backend: "nocodexperm", Worktree: db.ResidentWorkDir()}
	_, _, err := BuildCmd(task, cfg, false)
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "cannot enforce the resident permission policy")
}

func TestBuildCmd_ResidentMarkerNotInherited(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(ResidentEnvMarker, "1")
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	count := func(cmd *exec.Cmd) int {
		n := 0
		for _, kv := range cmd.Env {
			if strings.HasPrefix(kv, ResidentEnvMarker+"=") {
				n++
			}
		}
		return n
	}
	plain := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir()}
	cmd, cleanup, err := BuildCmd(plain, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	testutil.Equal(t, count(cmd), 0)

	res := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd2, cleanup2, err := BuildCmd(res, cfg, false)
	testutil.NoError(t, err)
	if cleanup2 != nil {
		t.Cleanup(cleanup2)
	}
	testutil.Equal(t, count(cmd2), 1)
}

// residentFixture builds an isolated HOME with a fake account dir (the default
// account's ~/.claude) and returns the resident task plus the account dir.
func residentFixture(t *testing.T) (*model.Task, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	acct := filepath.Join(home, ".claude")
	testutil.NoError(t, os.MkdirAll(acct, 0o755))
	prev := residentExecutable
	residentExecutable = func() (string, error) { return "/opt/argus/bin/argus", nil }
	t.Cleanup(func() { residentExecutable = prev })
	return &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir(), Prompt: "hi"}, acct
}

func buildResident(t *testing.T, task *model.Task, cfg config.Config) *exec.Cmd {
	t.Helper()
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	return cmd
}

func TestBuildCmd_ResidentIsSelfContainedUnderUserAllowAll(t *testing.T) {
	task, acct := residentFixture(t)
	// A user-level allow-all plus restrictions, model and effort.
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{
		"model": "opus",
		"effortLevel": "high",
		"permissions": {
			"allow": ["mcp__claude_ai_Linear__*", "Bash(*)", "mcp__claude_ai_Slack__*"],
			"ask": ["mcp__claude_ai_Notion__notion-fetch", "Bash(git push:*)"],
			"deny": ["Bash(rm -rf:*)", "mcp__claude_ai_Slack__slack_send_message"]
		},
		"sandbox": {"enabled": true, "autoAllowBashIfSandboxed": true, "excludedCommands": ["git"], "network": {"allowedDomains": ["example.com"], "deniedDomains": ["blocked.test"]}},
		"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "auto-approve.sh"}]}]}
		}`), 0o644))
	cmd := buildResident(t, task, residentCfg())
	line := strings.Join(cmd.Args, " ")
	testutil.Contains(t, line, "--setting-sources project ")
	if strings.Contains(line, "--setting-sources project,") || strings.Contains(line, "--setting-sources user") {
		t.Fatalf("setting sources must be exactly project: %s", line)
	}
	st, ok := settingsArg(t, cmd.Args)
	if !ok {
		t.Fatalf("no inline settings: %s", line)
	}
	perms, _ := st["permissions"].(map[string]any)
	testutil.Equal(t, perms["defaultMode"], any("default"))
	testutil.Equal(t, perms["disableBypassPermissionsMode"], any("disable"))
	join := func(v any) string {
		var out []string
		for _, x := range v.([]any) {
			out = append(out, x.(string))
		}
		return strings.Join(out, "\n")
	}
	allow, ask, deny := join(perms["allow"]), join(perms["ask"]), join(perms["deny"])
	for _, userAllow := range []string{"mcp__claude_ai_Linear__*", "Bash(*)", "mcp__claude_ai_Slack__*"} {
		if strings.Contains(allow, userAllow) || strings.Contains(ask, userAllow) || strings.Contains(deny, userAllow) {
			t.Fatalf("user allow rule %q leaked into the resident policy", userAllow)
		}
	}
	// User restrictions are re-supplied; the resident's own entries stay.
	testutil.Contains(t, deny, "Bash(rm -rf:*)")
	testutil.Contains(t, deny, "mcp__claude_ai_Slack__slack_send_message")
	testutil.Contains(t, ask, "Bash(git push:*)")
	testutil.Contains(t, ask, "mcp__claude_ai_Notion__notion-fetch")
	testutil.Contains(t, ask, "mcp__claude_ai_Google_Calendar__create_event")
	testutil.Contains(t, allow, "mcp__argus__kb_search")
	// The Slack send tool is listed once even though the user denied it too.
	testutil.Equal(t, strings.Count("\n"+deny+"\n", "\nmcp__claude_ai_Slack__slack_send_message\n"), 1)
	// Inline Stop hook replaces the dropped user-level one.
	hooks, _ := st["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	testutil.Equal(t, stop["type"], any("command"))
	testutil.Equal(t, stop["command"], any("/opt/argus/bin/argus coord-hook"))
	// Account model/effort are carried so the resident does not change model.
	testutil.Equal(t, st["model"], any("opus"))
	testutil.Equal(t, st["effortLevel"], any("high"))
	// The user sandbox block is carried for its restrictions only: the keys
	// that would auto-approve or unsandbox Bash are neutralized.
	sb, _ := st["sandbox"].(map[string]any)
	testutil.Equal(t, sb["enabled"], any(true))
	testutil.Equal(t, sb["autoAllowBashIfSandboxed"], any(false))
	testutil.Equal(t, sb["allowUnsandboxedCommands"], any(false))
	if _, has := sb["excludedCommands"]; has {
		t.Fatalf("excludedCommands must not be carried: %v", sb)
	}
	// allowedDomains loosens, so it is dropped; deniedDomains restricts, so it is kept.
	if strings.Contains(line, "example.com") {
		t.Fatalf("network.allowedDomains must not be carried: %s", line)
	}
	testutil.Contains(t, line, "blocked.test")
	// User hooks are never carried: only the inline Stop coord-hook exists.
	testutil.Equal(t, len(hooks), 1)
	if strings.Contains(line, "auto-approve.sh") {
		t.Fatalf("user hook leaked into the resident: %s", line)
	}
}

func TestReadResidentUserSettings_SandboxAndSkippedHooks(t *testing.T) {
	task, acct := residentFixture(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{
		"sandbox": {"enabled": true},
		"hooks": {"Stop": [], "PreToolUse": [], "PermissionRequest": []}}`), 0o644))
	us := readResidentUserSettings(task, residentCfg(), false)
	testutil.Equal(t, string(us.Sandbox), `{"allowUnsandboxedCommands":false,"autoAllowBashIfSandboxed":false,"enabled":true}`)
	// Under Argus's own sandbox `enabled` is not carried, so nothing remains.
	us = readResidentUserSettings(task, residentCfg(), true)
	testutil.Equal(t, len(us.Sandbox), 0)
	us = readResidentUserSettings(task, residentCfg(), false)
	testutil.DeepEqual(t, us.SkippedHooks, []string{"PermissionRequest", "PreToolUse", "Stop"})
	// A non-object sandbox value is ignored rather than injected.
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{"sandbox": true}`), 0o644))
	us = readResidentUserSettings(task, residentCfg(), false)
	testutil.Equal(t, len(us.Sandbox), 0)
	testutil.Equal(t, len(us.SkippedHooks), 0)
}

// syncBuf is a mutex-guarded writer so a slog handler can be read safely.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestReadResidentUserSettings_LogsSkippedHooks(t *testing.T) {
	task, acct := residentFixture(t)
	var logs syncBuf
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"),
		[]byte(`{"hooks": {"Stop": [], "PreToolUse": [], "PermissionRequest": []}}`), 0o644))
	readResidentUserSettings(task, residentCfg(), false)
	testutil.Contains(t, logs.String(), "[resident] user hooks not applied")
	testutil.Contains(t, logs.String(), "PermissionRequest,PreToolUse,Stop")

	// No hooks: nothing is logged.
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{}`), 0o644))
	before := logs.String()
	readResidentUserSettings(task, residentCfg(), false)
	testutil.Equal(t, logs.String(), before)
}

func TestResidentSandbox_Robustness(t *testing.T) {
	for name, raw := range map[string]string{
		"null":      `null`,
		"empty":     ``,
		"true":      `true`,
		"string":    `"x"`,
		"array":     `[1]`,
		"number":    `3`,
		"malformed": `{"a":`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := residentSandbox(json.RawMessage(raw), false); got != nil {
				t.Fatalf("want nil, got %s", got)
			}
		})
	}
	t.Run("empty object carries nothing", func(t *testing.T) {
		if got := residentSandbox(json.RawMessage(`{}`), false); got != nil {
			t.Fatalf("want nil, got %s", got)
		}
	})
	t.Run("through settings.json null", func(t *testing.T) {
		task, acct := residentFixture(t)
		testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{"sandbox": null}`), 0o644))
		testutil.Equal(t, len(readResidentUserSettings(task, residentCfg(), false).Sandbox), 0)
	})
}

// A backend CLAUDE_CONFIG_DIR mapping that resolves to unset launches on
// Claude's default dir, so the resident must read ~/.claude, not skip it.
func TestResidentAccountDir_UnsetBackendMappingFallsBackToDefault(t *testing.T) {
	task, acct := residentFixture(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"),
		[]byte(`{"permissions":{"deny":["Bash(rm:*)"]}}`), 0o644))
	cfg := residentCfg()
	b := cfg.Backends["claude"]
	b.EnvVars = map[string]string{"CLAUDE_CONFIG_DIR": "R12_UNSET_VAR"}
	cfg.Backends["claude"] = b
	t.Setenv("R12_UNSET_VAR", "")
	_ = os.Unsetenv("R12_UNSET_VAR")

	testutil.Equal(t, residentAccountDir(task, cfg), config.DefaultClaudeDir())
	testutil.Equal(t, residentAccountDir(task, cfg), acct)
	st, ok := settingsArg(t, buildResident(t, task, cfg).Args)
	testutil.True(t, ok)
	perms, _ := st["permissions"].(map[string]any)
	deny, _ := perms["deny"].([]any)
	testutil.True(t, slices.Contains(deny, any("Bash(rm:*)")))
}

func TestBuildCmd_ResidentModelPrecedenceAndMissingSettings(t *testing.T) {
	task, acct := residentFixture(t)
	// No settings.json at all: just the resident's own lists, no model.
	st, _ := settingsArg(t, buildResident(t, task, residentCfg()).Args)
	if _, has := st["model"]; has {
		t.Fatalf("no account model to carry: %v", st)
	}
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{"model":"opus"}`), 0o644))
	// An explicit backend --model wins; the account model is not injected.
	cfg := residentCfg()
	cfg.Backends["claude"] = config.Backend{Command: "claude --model sonnet"}
	st, _ = settingsArg(t, buildResident(t, task, cfg).Args)
	if _, has := st["model"]; has {
		t.Fatalf("explicit --model must win over the account model: %v", st)
	}
	// Unparseable settings fall back to the resident's own rules.
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{nope`), 0o644))
	st, ok := settingsArg(t, buildResident(t, task, residentCfg()).Args)
	if !ok {
		t.Fatal("resident policy must still be delivered")
	}
	if _, has := st["model"]; has {
		t.Fatalf("unparseable settings carry nothing: %v", st)
	}
}

func TestBuildCmd_ResidentGetsUserClaudeMDAndSkills(t *testing.T) {
	task, acct := residentFixture(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "CLAUDE.md"), []byte("USER-RULE-MARKER be terse"), 0o644))
	testutil.NoError(t, os.MkdirAll(filepath.Join(acct, "skills", "lynx"), 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "skills", "lynx", "SKILL.md"), []byte("---\nname: lynx\n---\n"), 0o644))
	routing := filepath.Join(t.TempDir(), "routing.md")
	testutil.NoError(t, os.WriteFile(routing, []byte("ROUTING-MARKER"), 0o644))
	defer SetEnsureBuiltinRoutingForTest(func() (string, error) { return routing, nil })()

	cmd := buildResident(t, task, residentCfg())
	line := strings.Join(cmd.Args, " ")
	// Exactly one prompt file (Claude keeps only the last), holding both.
	testutil.Equal(t, strings.Count(line, "--append-system-prompt-file"), 1)
	_, rest, _ := strings.Cut(line, "--append-system-prompt-file ")
	path := strings.Trim(strings.Fields(rest)[0], "'")
	body, err := os.ReadFile(path)
	testutil.NoError(t, err)
	testutil.Contains(t, string(body), "ROUTING-MARKER")
	testutil.Contains(t, string(body), "USER-RULE-MARKER be terse")
	// Skills: an add-dir whose .claude/skills resolves to the account's skills.
	var skillRoot string
	for _, f := range strings.Fields(line) {
		f = strings.Trim(f, "'")
		if link, err := os.Readlink(filepath.Join(f, ".claude", "skills")); err == nil {
			testutil.Equal(t, link, filepath.Join(acct, "skills"))
			skillRoot = f
		}
	}
	if skillRoot == "" {
		t.Fatalf("no --add-dir exposing the user skills: %s", line)
	}

	// A plain task gets neither the CLAUDE.md nor the skills link.
	plain := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir()}
	pcmd := buildResident(t, plain, residentCfg())
	pline := strings.Join(pcmd.Args, " ")
	if strings.Contains(pline, skillRoot) || strings.Contains(pline, "--setting-sources") {
		t.Fatalf("plain task must be unchanged: %s", pline)
	}
	pb, _ := os.ReadFile(routing)
	testutil.Equal(t, string(pb), "ROUTING-MARKER")
}

func TestBuildCmd_ResidentMissingClaudeMDAndSkillsSkipped(t *testing.T) {
	task, _ := residentFixture(t)
	routing := filepath.Join(t.TempDir(), "routing.md")
	testutil.NoError(t, os.WriteFile(routing, []byte("ROUTING-MARKER"), 0o644))
	defer SetEnsureBuiltinRoutingForTest(func() (string, error) { return routing, nil })()
	line := strings.Join(buildResident(t, task, residentCfg()).Args, " ")
	testutil.Contains(t, line, "--append-system-prompt-file "+shellQuote(routing))
	if strings.Contains(line, residentLaunchDirName) {
		t.Fatalf("nothing to add: %s", line)
	}
}

func TestResidentStopHookCommand(t *testing.T) {
	prev := residentExecutable
	t.Cleanup(func() { residentExecutable = prev })
	for name, tc := range map[string]struct {
		exe  string
		err  error
		want string
	}{
		"plain":  {"/usr/local/bin/argus", nil, "/usr/local/bin/argus coord-hook"},
		"spaces": {"/Users/a b/argus", nil, "'/Users/a b/argus' coord-hook"},
		"error":  {"", os.ErrNotExist, "argus coord-hook"},
	} {
		t.Run(name, func(t *testing.T) {
			residentExecutable = func() (string, error) { return tc.exe, tc.err }
			testutil.Equal(t, residentStopHookCommand(), tc.want)
		})
	}
}

// An inherited CLAUDE_CONFIG_DIR is stripped from the spawned env, so the
// default-account resident must read ~/.claude, never that directory.
func TestBuildCmd_ResidentIgnoresInheritedClaudeConfigDir(t *testing.T) {
	task, acct := residentFixture(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "CLAUDE.md"), []byte("DEFAULT-DIR-RULE"), 0o644))
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{"model":"default-dir-model"}`), 0o644))
	other := t.TempDir()
	testutil.NoError(t, os.WriteFile(filepath.Join(other, "CLAUDE.md"), []byte("INHERITED-DIR-RULE"), 0o644))
	testutil.NoError(t, os.WriteFile(filepath.Join(other, "settings.json"), []byte(`{"model":"inherited-dir-model"}`), 0o644))
	t.Setenv("CLAUDE_CONFIG_DIR", other)
	routing := filepath.Join(t.TempDir(), "routing.md")
	testutil.NoError(t, os.WriteFile(routing, []byte("ROUTING"), 0o644))
	defer SetEnsureBuiltinRoutingForTest(func() (string, error) { return routing, nil })()

	testutil.Equal(t, residentAccountDir(task, residentCfg()), acct)
	cmd := buildResident(t, task, residentCfg())
	line := strings.Join(cmd.Args, " ")
	_, rest, _ := strings.Cut(line, "--append-system-prompt-file ")
	body, err := os.ReadFile(strings.Trim(strings.Fields(rest)[0], "'"))
	testutil.NoError(t, err)
	testutil.Contains(t, string(body), "DEFAULT-DIR-RULE")
	testutil.False(t, strings.Contains(string(body), "INHERITED-DIR-RULE"))
	testutil.False(t, strings.Contains(line, "inherited-dir-model"))
}

// A tampered prompt file must be restored (atomically, replacing even a symlink).
func TestMaterializeResidentPrompt_RestoresTamperedFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first, err := materializeResidentPrompt("", "RULES")
	testutil.NoError(t, err)
	want, err := os.ReadFile(first)
	testutil.NoError(t, err)

	t.Run("modified bytes", func(t *testing.T) {
		testutil.NoError(t, os.WriteFile(first, []byte("allow everything"), 0o644))
		got, err := materializeResidentPrompt("", "RULES")
		testutil.NoError(t, err)
		testutil.Equal(t, got, first)
		b, err := os.ReadFile(got)
		testutil.NoError(t, err)
		testutil.Equal(t, string(b), string(want))
	})
	t.Run("symlink swapped in", func(t *testing.T) {
		evil := filepath.Join(t.TempDir(), "evil.md")
		testutil.NoError(t, os.WriteFile(evil, []byte("evil"), 0o644))
		testutil.NoError(t, os.Remove(first))
		testutil.NoError(t, os.Symlink(evil, first))
		got, err := materializeResidentPrompt("", "RULES")
		testutil.NoError(t, err)
		b, err := os.ReadFile(got)
		testutil.NoError(t, err)
		testutil.Equal(t, string(b), string(want))
		ev, _ := os.ReadFile(evil)
		testutil.Equal(t, string(ev), "evil") // never written through
	})
	t.Run("deleted file", func(t *testing.T) {
		testutil.NoError(t, os.Remove(first))
		_, err := materializeResidentPrompt("", "RULES")
		testutil.NoError(t, err)
		b, err := os.ReadFile(first)
		testutil.NoError(t, err)
		testutil.Equal(t, string(b), string(want))
	})
}

// settingsFromShell evaluates BuildCmd's real sh -c script under sh and
// decodes the --settings argv entry, so a quoting regression that sh splits or
// mangles fails here.
func settingsFromShell(t *testing.T, args []string) ([]string, map[string]any) {
	t.Helper()
	argv := shellArgv(t, args[len(args)-1])
	i := slices.Index(argv, "--settings")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("no --settings in argv %q", argv)
	}
	var st map[string]any
	testutil.NoError(t, json.Unmarshal([]byte(argv[i+1]), &st))
	return argv, st
}

func TestBuildCmd_ResidentSettingsShellOracle(t *testing.T) {
	task, acct := residentFixture(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{
		"permissions": {"deny": ["Bash(echo 'it''s' $HOME \"q\")"]}}`), 0o644))
	argv, st := settingsFromShell(t, buildResident(t, task, residentCfg()).Args)
	j := slices.Index(argv, "--setting-sources")
	if j < 0 || j+1 >= len(argv) || argv[j+1] != "project" {
		t.Fatalf("setting sources argv: %q", argv)
	}
	deny := st["permissions"].(map[string]any)["deny"].([]any)
	testutil.Equal(t, slices.Contains(deny, any(`Bash(echo 'it''s' $HOME "q")`)), true)
}

func TestBuildCmd_ResidentUsesItsOwnAccountFiles(t *testing.T) {
	task, defaultAcct := residentFixture(t)
	work := filepath.Join(os.Getenv("HOME"), "claude-work")
	testutil.NoError(t, os.MkdirAll(filepath.Join(work, "skills", "wk"), 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(work, "skills", "wk", "SKILL.md"), []byte("---\nname: wk\n---\n"), 0o644))
	testutil.NoError(t, os.MkdirAll(filepath.Join(defaultAcct, "skills", "dflt"), 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(defaultAcct, "skills", "dflt", "SKILL.md"), []byte("---\nname: dflt\n---\n"), 0o644))
	testutil.NoError(t, os.WriteFile(filepath.Join(defaultAcct, "CLAUDE.md"), []byte("DEFAULT-ACCT-MD"), 0o644))
	testutil.NoError(t, os.WriteFile(filepath.Join(work, "CLAUDE.md"), []byte("WORK-ACCT-MD"), 0o644))
	testutil.NoError(t, os.WriteFile(filepath.Join(defaultAcct, "settings.json"), []byte(`{"permissions":{"deny":["DEFAULT-DENY"]}}`), 0o644))
	testutil.NoError(t, os.WriteFile(filepath.Join(work, "settings.json"), []byte(`{"permissions":{"deny":["WORK-DENY"]}}`), 0o644))
	cfg := residentCfg()
	cfg.Accounts = map[string]config.Account{"work": {ClaudeConfigDir: work, Inherit: []string{}}}
	task.Account = "work"
	line := strings.Join(buildResident(t, task, cfg).Args, " ")
	_, rest, _ := strings.Cut(line, "--append-system-prompt-file ")
	body, err := os.ReadFile(strings.Trim(strings.Fields(rest)[0], "'"))
	testutil.NoError(t, err)
	testutil.Contains(t, string(body), "WORK-ACCT-MD")
	testutil.Equal(t, strings.Contains(string(body), "DEFAULT-ACCT-MD"), false)
	testutil.Contains(t, line, "WORK-DENY")
	testutil.Equal(t, strings.Contains(line, "DEFAULT-DENY"), false)
	var linked string
	for _, f := range strings.Fields(line) {
		f = strings.Trim(f, "'")
		if l, err := os.Readlink(filepath.Join(f, ".claude", "skills")); err == nil {
			linked = l
		}
	}
	testutil.Equal(t, linked, filepath.Join(work, "skills"))
}

func TestBuildCmd_PlainTaskGetsRoutingNotResidentPrompt(t *testing.T) {
	_, acct := residentFixture(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "CLAUDE.md"), []byte("USER-RULE-MARKER"), 0o644))
	routing := filepath.Join(t.TempDir(), "routing.md")
	testutil.NoError(t, os.WriteFile(routing, []byte("ROUTING-MARKER"), 0o644))
	defer SetEnsureBuiltinRoutingForTest(func() (string, error) { return routing, nil })()
	plain := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir()}
	pline := strings.Join(buildResident(t, plain, residentCfg()).Args, " ")
	testutil.Contains(t, pline, "--append-system-prompt-file "+shellQuote(routing))
	testutil.Equal(t, strings.Contains(pline, residentLaunchDirName), false)
}

func TestBuildCmd_ResidentTaskModelWinsOverAccountModel(t *testing.T) {
	task, acct := residentFixture(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{"model":"opus"}`), 0o644))
	task.Model = "sonnet"
	cmd := buildResident(t, task, residentCfg())
	testutil.Contains(t, strings.Join(cmd.Args, " "), "--model 'sonnet'")
	st, _ := settingsArg(t, cmd.Args)
	if _, has := st["model"]; has {
		t.Fatalf("resolved model must win over the account model: %v", st["model"])
	}
}

func TestBuildCmd_BackendEnvVarsCannotForgeResidentMarker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("R11_FORGE_SRC", "1")
	cfg := residentCfg()
	b := cfg.Backends["claude"]
	b.EnvVars = map[string]string{ResidentEnvMarker: "R11_FORGE_SRC"}
	cfg.Backends["claude"] = b
	plain := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir()}
	cmd := buildResident(t, plain, cfg)
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, ResidentEnvMarker+"=") {
			t.Fatalf("backend env_vars forged %s", kv)
		}
	}
}

// sandboxWriteRoots parses the generated SBPL profile for a task sandboxed on
// worktree and returns every file-write* subpath/prefix/literal it grants.
func sandboxWriteRoots(t *testing.T, home, worktree string) []string {
	t.Helper()
	path, _, cleanup, err := GenerateSandboxConfig(worktree, config.SandboxConfig{})
	testutil.NoError(t, err)
	defer cleanup()
	raw, err := os.ReadFile(path)
	testutil.NoError(t, err)
	grant := regexp.MustCompile(`\(allow file-write\* \((?:subpath|prefix|literal) (.+)\)\)`)
	var roots []string
	for _, m := range grant.FindAllStringSubmatch(string(raw), -1) {
		expr := m[1]
		switch {
		case strings.HasPrefix(expr, `(param "WORKTREE")`):
			roots = append(roots, evalSymlinksOrKeep(worktree))
		case strings.HasPrefix(expr, `(string-append (param "HOME")`):
			rel := regexp.MustCompile(`"([^"]*)"\)$`).FindStringSubmatch(expr)
			if rel != nil {
				roots = append(roots, filepath.Join(evalSymlinksOrKeep(home), rel[1]))
			}
		case strings.HasPrefix(expr, `"`):
			roots = append(roots, strings.Trim(expr, `"`))
		}
	}
	return roots
}

func under(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// The resident's launch files must live where no sandboxed agent can write,
// including the resident's own sandbox (its worktree grant) and every other
// task's (~/.argus/cache and an arbitrary worktree).
func TestResidentLaunchDir_NotWritableBySandbox(t *testing.T) {
	task, acct := residentFixture(t)
	home, _ := os.UserHomeDir()
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "CLAUDE.md"), []byte("x"), 0o644))
	testutil.NoError(t, os.MkdirAll(filepath.Join(acct, "skills"), 0o755))
	prompt, err := materializeResidentPrompt("", "x")
	testutil.NoError(t, err)
	skills := ensureResidentUserSkillsDir(task, residentCfg())
	testutil.True(t, skills != "")
	dir, err := residentLaunchDir()
	testutil.NoError(t, err)

	fi, err := os.Lstat(dir)
	testutil.NoError(t, err)
	testutil.Equal(t, fi.Mode().Perm(), os.FileMode(0o700))
	pfi, err := os.Lstat(prompt)
	testutil.NoError(t, err)
	testutil.Equal(t, pfi.Mode().Perm(), os.FileMode(0o600))

	realHome := evalSymlinksOrKeep(home)
	for _, p := range []string{evalSymlinksOrKeep(prompt), evalSymlinksOrKeep(skills)} {
		testutil.False(t, under(p, filepath.Join(realHome, ".argus", "cache")))
		testutil.False(t, under(p, evalSymlinksOrKeep(db.ResidentWorkDir())))
		testutil.False(t, under(p, evalSymlinksOrKeep(WorktreeDir("proj", "task"))))
	}
	for name, wt := range map[string]string{
		"resident": db.ResidentWorkDir(),
		"task":     WorktreeDir("proj", "task"),
	} {
		t.Run(name, func(t *testing.T) {
			testutil.NoError(t, os.MkdirAll(wt, 0o755))
			roots := sandboxWriteRoots(t, home, wt)
			testutil.True(t, slices.ContainsFunc(roots, func(r string) bool { return strings.HasSuffix(r, "/.argus/cache") }))
			for _, r := range roots {
				if under(realHome, r) {
					continue // test HOME lives in a temp dir; real HOMEs are not under a temp grant
				}
				for _, p := range []string{evalSymlinksOrKeep(prompt), evalSymlinksOrKeep(skills), evalSymlinksOrKeep(dir)} {
					if under(p, r) {
						t.Fatalf("%s is covered by sandbox write grant %s", p, r)
					}
				}
			}
		})
	}
}

// A symlink pre-planted at the launch dir, the prompt file, or the skills link
// is replaced, never followed, and its target stays untouched.
func TestResidentLaunch_PlantedSymlinksReplacedNotFollowed(t *testing.T) {
	t.Run("launch dir is a symlink", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		victim := t.TempDir()
		testutil.NoError(t, os.MkdirAll(filepath.Join(home, ".argus"), 0o755))
		testutil.NoError(t, os.Symlink(victim, filepath.Join(home, ".argus", residentLaunchDirName)))
		file, err := materializeResidentPrompt("", "RULES")
		testutil.NoError(t, err)
		fi, err := os.Lstat(filepath.Dir(file))
		testutil.NoError(t, err)
		testutil.True(t, fi.IsDir() && fi.Mode()&os.ModeSymlink == 0)
		ents, _ := os.ReadDir(victim)
		testutil.Equal(t, len(ents), 0)
	})
	t.Run("prompt file is a symlink", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		file, err := materializeResidentPrompt("", "RULES")
		testutil.NoError(t, err)
		victim := filepath.Join(t.TempDir(), "victim.md")
		testutil.NoError(t, os.WriteFile(victim, []byte("evil"), 0o644))
		testutil.NoError(t, os.Remove(file))
		testutil.NoError(t, os.Symlink(victim, file))
		got, err := materializeResidentPrompt("", "RULES")
		testutil.NoError(t, err)
		fi, err := os.Lstat(got)
		testutil.NoError(t, err)
		testutil.True(t, fi.Mode().IsRegular())
		b, _ := os.ReadFile(victim)
		testutil.Equal(t, string(b), "evil")
		cur, _ := os.ReadFile(got)
		testutil.Contains(t, string(cur), "RULES")
	})
	t.Run("skills link parents are planted", func(t *testing.T) {
		task, acct := residentFixture(t)
		testutil.NoError(t, os.MkdirAll(filepath.Join(acct, "skills"), 0o755))
		root := ensureResidentUserSkillsDir(task, residentCfg())
		testutil.True(t, root != "")
		victim := t.TempDir()
		// Replace .claude with a symlink to a victim dir.
		testutil.NoError(t, os.RemoveAll(filepath.Join(root, ".claude")))
		testutil.NoError(t, os.Symlink(victim, filepath.Join(root, ".claude")))
		root2 := ensureResidentUserSkillsDir(task, residentCfg())
		testutil.Equal(t, root2, root)
		ents, _ := os.ReadDir(victim)
		testutil.Equal(t, len(ents), 0)
		link, err := os.Readlink(filepath.Join(root, ".claude", "skills"))
		testutil.NoError(t, err)
		testutil.Equal(t, link, filepath.Join(acct, "skills"))
		// A planted wrong link target is replaced.
		testutil.NoError(t, os.Remove(filepath.Join(root, ".claude", "skills")))
		testutil.NoError(t, os.Symlink(victim, filepath.Join(root, ".claude", "skills")))
		ensureResidentUserSkillsDir(task, residentCfg())
		link, _ = os.Readlink(filepath.Join(root, ".claude", "skills"))
		testutil.Equal(t, link, filepath.Join(acct, "skills"))
	})
}

func TestResidentSandbox_Allowlist(t *testing.T) {
	// Every loosening key (and an unknown future one) is dropped; every
	// allowlisted restricting key survives; the two Bash keys are forced false.
	const in = `{
		"enabled": true, "failIfUnavailable": true,
		"autoAllowBashIfSandboxed": true, "allowUnsandboxedCommands": true,
		"excludedCommands": ["git"], "allowAppleEvents": true,
		"enableWeakerNetworkIsolation": true, "allowNestedUserNamespaces": true,
		"futureLooseningKey": {"x": 1},
		"credentials": {"allowPlaintextInject": true, "files": [{"path": "~/.aws", "mode": "deny"}]},
		"network": {"allowedDomains": ["a.test"], "allowLocalBinding": true, "allowUnixSockets": ["/x"],
			"deniedDomains": ["d.test", 5, ""]},
		"filesystem": {"disabled": true, "allowRead": ["/r"], "allowWrite": ["/w"],
			"denyRead": ["~/.ssh"], "denyWrite": ["/etc"]}}`
	var got map[string]any
	testutil.NoError(t, json.Unmarshal(residentSandbox(json.RawMessage(in), false), &got))
	want := map[string]any{
		"enabled": true, "failIfUnavailable": true,
		"autoAllowBashIfSandboxed": false, "allowUnsandboxedCommands": false,
		"network":    map[string]any{"deniedDomains": []any{"d.test"}},
		"filesystem": map[string]any{"denyRead": []any{"~/.ssh"}, "denyWrite": []any{"/etc"}},
	}
	testutil.DeepEqual(t, got, want)

	for name, loosening := range map[string]string{
		"excludedCommands":    `{"excludedCommands":["git"]}`,
		"allowAppleEvents":    `{"allowAppleEvents":true}`,
		"filesystem.disabled": `{"filesystem":{"disabled":true}}`,
		"weaker isolation":    `{"enableWeakerNetworkIsolation":true}`,
		"nested userns":       `{"allowNestedUserNamespaces":true}`,
		"network.allowed":     `{"network":{"allowedDomains":["a.test"]}}`,
		"plaintext inject":    `{"credentials":{"allowPlaintextInject":true}}`,
		"unknown future key":  `{"newThing":true}`,
		"enabled false":       `{"enabled":false}`,
		"autoAllow only":      `{"autoAllowBashIfSandboxed":true,"allowUnsandboxedCommands":true}`,
		"non-bool enabled":    `{"enabled":"yes"}`,
		"mistyped deniedlist": `{"network":{"deniedDomains":"x"}}`,
	} {
		t.Run("alone drops "+name, func(t *testing.T) {
			if got := residentSandbox(json.RawMessage(loosening), false); got != nil {
				t.Fatalf("want nil (nothing restrictive), got %s", got)
			}
		})
	}
}

func TestResidentSandbox_UnderArgusSandboxDropsEnabled(t *testing.T) {
	const in = `{"enabled":true,"failIfUnavailable":true,"filesystem":{"denyRead":["~/.ssh"]}}`
	var got map[string]any
	testutil.NoError(t, json.Unmarshal(residentSandbox(json.RawMessage(in), true), &got))
	if _, has := got["enabled"]; has {
		t.Fatalf("enabled must not be carried under Argus's sandbox: %v", got)
	}
	if _, has := got["failIfUnavailable"]; has {
		t.Fatalf("failIfUnavailable must not be carried under Argus's sandbox: %v", got)
	}
	testutil.Equal(t, got["allowUnsandboxedCommands"], any(false))
	testutil.DeepEqual(t, got["filesystem"], any(map[string]any{"denyRead": []any{"~/.ssh"}}))
	// Only enabled/failIfUnavailable in the block: nothing remains.
	if g := residentSandbox(json.RawMessage(`{"enabled":true,"failIfUnavailable":true}`), true); g != nil {
		t.Fatalf("want nil, got %s", g)
	}
}

func TestBuildCmd_ResidentSandboxedTaskDropsEnabled(t *testing.T) {
	task, acct := residentFixture(t)
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"),
		[]byte(`{"sandbox":{"enabled":true,"failIfUnavailable":true,"network":{"deniedDomains":["blocked.test"]}}}`), 0o644))
	sandboxOf := func(cfg config.Config) map[string]any {
		t.Helper()
		st, _ := settingsArg(t, buildResident(t, task, cfg).Args)
		sb, _ := st["sandbox"].(map[string]any)
		return sb
	}

	// No Argus wrap: the user's own Claude sandbox stays on.
	testutil.Equal(t, sandboxOf(residentCfg())["enabled"], any(true))

	// A persisted task.Sandboxed alone must NOT drop it: the wrap decision is
	// made from the live config, and dropping `enabled` without a real wrap
	// would leave the resident with neither sandbox (fail-open).
	task.Sandboxed = true
	testutil.Equal(t, sandboxOf(residentCfg())["enabled"], any(true))

	// Sandbox enabled in config but unavailable on the platform: no wrap, so
	// the user's own Claude sandbox must stay on (never neither).
	cfg := residentCfg()
	cfg.Sandbox.Enabled = true
	sandboxAvailabilityForSignalTest(t, false)
	testutil.Equal(t, sandboxOf(cfg)["enabled"], any(true))

	// Argus really wraps the launch: `enabled` / failIfUnavailable drop (no
	// nesting), the restrictive keys stay. The seam fakes availability, so no
	// sandbox-exec binary is needed to build the command.
	sandboxAvailabilityForSignalTest(t, true)
	wrapped := buildResident(t, task, cfg)
	testutil.Equal(t, wrapped.Args[0], "sh")
	words, err := splitShellWords(wrapped.Args[2])
	testutil.NoError(t, err)
	testutil.True(t, len(words) > 2)
	testutil.Equal(t, words[0].value, sandboxExecPath)
	inner := words[len(words)-1].value
	testutil.Equal(t, words[len(words)-2].value, "-c")
	st, ok := settingsArg(t, []string{inner})
	testutil.True(t, ok)
	sb, _ := st["sandbox"].(map[string]any)
	if _, has := sb["enabled"]; has {
		t.Fatalf("enabled carried while Argus wraps the resident: %v", sb)
	}
	if _, has := sb["failIfUnavailable"]; has {
		t.Fatalf("failIfUnavailable carried while Argus wraps the resident: %v", sb)
	}
	testutil.Contains(t, fmt.Sprint(sb["network"]), "blocked.test")

	// Profile generation fails (temp dir unusable): the resident refuses to
	// start rather than launch with neither sandbox; a plain task falls through.
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	_, cleanup, err := BuildCmd(task, cfg, false)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("resident must fail closed when its sandbox profile cannot be generated")
	}
	testutil.Contains(t, err.Error(), "resident sandbox profile")
	plain := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir(), Prompt: "hi"}
	pcmd, pcleanup, perr := BuildCmd(plain, cfg, false)
	testutil.NoError(t, perr)
	if pcleanup != nil {
		pcleanup()
	}
	testutil.False(t, strings.Contains(pcmd.Args[2], sandboxExecPath))
}

func TestCheckClaudeResidentCommand(t *testing.T) {
	ok := []string{
		"claude",
		"claude --model sonnet",
		"/opt/x/claude --model=opus",
		"claude --effort high",
		"claude --effort=low --verbose",
		"claude --fallback-model sonnet --model opus",
		"~/.local/bin/claude --model sonnet",
		"claude --model 'opus[1m]'",
		"claude --model 'claude-opus-4-1'",
		"claude --effort='high'",
		"claude --model='opus[1m]' --verbose",
		"claude \\\n --model x", // a backslash-newline continuation is one line to sh
	}
	for _, c := range ok {
		t.Run("ok "+c, func(t *testing.T) { testutil.NoError(t, checkClaudeResidentCommand(c)) })
	}
	refused := []struct{ cmd, names string }{
		{"claude --verbose\n--model opus", "one line"},
		{"claude --model opus\n", "one line"},
		{"claude\n--verbose", "one line"},
		{"claude --model x\r--verbose", "one line"},
		{"-a/x/claude", "plain path"},
		{"+x/claude", "plain path"},
		{"%/claude", "plain path"},
		{"claude --allowedTools Read", "--allowedTools"},
		{"claude --allowed-tools=Read", "--allowed-tools=Read"},
		{"claude --plugin-dir /x", "--plugin-dir"},
		{"claude --plugin-url http://x", "--plugin-url"},
		{"claude --agent foo", "--agent"},
		{"claude --agents {}", "--agents"},
		{"claude --mcp-config /x.json", "--mcp-config"},
		{"claude --settings /x.json", "--settings"},
		{"claude --setting-sources user", "--setting-sources"},
		{"claude --add-dir /x", "--add-dir"},
		{"claude --disallowedTools Bash", "--disallowedTools"},
		{"claude positional", "positional"},
		{"claude --model", "--model"},
		{"claude --model --verbose", "--model"},
		{"claude --model x; rm -rf y", ";"},
		{"claude --model opus && ls", "&&"},
		{"claude --model $(id)", "$(id)"},
		{"claude --model `id`", "`id`"},
		{"claude | tee", "|"},
		{`/opt/'x'/claude`, "plain path"},
		{`~/'.local'/bin/claude`, "plain path"},
		{`'/opt/x/claude'`, "plain path"},
		{`"/opt/x/claude" --model x`, "plain path"},
		{`/opt/[x]/claude`, "plain path"},
		{`/opt/*/claude`, "plain path"},
		{`/opt/?/claude`, "plain path"},
		{`/opt/x/\claude`, "plain path"},
		{`/opt/x\ y/claude`, "plain path"},
		{`A=/opt/claude --model x`, "plain path"},
		{"claude --model opus[1m]", "single-quote"},
		{"claude --model=opus[1m]", "single-quote"},
		{`claude --model "opus"`, "not allowed"},
		{`claude --model="opus"`, "not allowed"},
		{"claude --model 'opus", "unterminated"},
		{"claude --model 'a b'", "'a b'"},
		{"claude --model 'a$b'", "'a$b'"},
		{"claude --model 'a;b'", "'a;b'"},
		{"$HOME/.local/bin/claude --model x", "$HOME/.local/bin/claude"},
		{"~root/bin/claude --model x", "~root/bin/claude"},
		{"claude ~/x --model y", "~/x"},
		{"claude --model ~/x", "~/x"},
		{"~/bin/sh --model x", "sh"},
		{"x;/claude --model y", "x;/claude"},
		{"$(id)/claude --model y", "$(id)/claude"},
		{"claude --model=$(id)", "--model=$(id)"},
		{"claude --effort=x;rm", "--effort=x;rm"},
		{`claude "--allowedTools" Read`, "--allowedTools"},
		{"codex --model x", "codex"},
		{"/bin/sh --model x", "sh"},
	}
	for _, tt := range refused {
		t.Run("refused "+tt.cmd, func(t *testing.T) {
			err := checkClaudeResidentCommand(tt.cmd)
			if err == nil {
				t.Fatalf("expected refusal")
			}
			testutil.Contains(t, err.Error(), tt.names)
			if strings.Contains(tt.cmd, "[1m]") {
				testutil.Contains(t, err.Error(), "single-quote")
			} else {
				testutil.False(t, strings.Contains(err.Error(), "single-quote"))
				// A refused word (not a flag/executable/parse error) gets the
				// generic wording, not the bracket hint.
				if strings.HasPrefix(err.Error(), "word ") || strings.Contains(err.Error(), "\": word ") {
					testutil.Contains(t, err.Error(), "quotes, spaces and shell operators")
				}
			}
		})
	}
}

func TestBuildCmd_ResidentRefusesDisallowedFlagsAndSurfacesError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	for _, flag := range []string{"--allowedTools Read", "--plugin-dir /x", "--mcp-config /x.json", "--settings /x.json", "--add-dir /x"} {
		t.Run(flag, func(t *testing.T) {
			cfg := residentCfg()
			cfg.Backends["claude"] = config.Backend{Command: "claude --dangerously-skip-permissions " + flag}
			_, _, err := BuildCmd(task, cfg, false)
			if err == nil {
				t.Fatal("expected refusal")
			}
			name, _, _ := strings.Cut(flag, " ")
			testutil.Contains(t, err.Error(), name)
			// The offending flag leads the message (Settings shows it truncated).
			testutil.True(t, strings.HasPrefix(err.Error(), `flag "`+name+`" not allowed for the resident`))
			// A plain task with the same command is unaffected.
			plain := &model.Task{ID: "p", Name: "plain", Worktree: t.TempDir()}
			if _, cleanup, perr := BuildCmd(plain, cfg, false); perr != nil {
				t.Fatalf("plain task must be unaffected: %v", perr)
			} else if cleanup != nil {
				cleanup()
			}
		})
	}
	cfg := residentCfg()
	cfg.Backends["claude"] = config.Backend{Command: "/opt/x/claude --model=opus"}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		cleanup()
	}
	testutil.Contains(t, strings.Join(cmd.Args, " "), "--model=opus")
}

// Widening the resident's flag allowlist must be a deliberate test change.
func TestResidentClaudeFlagAllowlist_Pinned(t *testing.T) {
	testutil.DeepEqual(t, residentClaudeValueFlags, []string{"--model", "--effort", "--fallback-model"})
	testutil.DeepEqual(t, residentClaudeBoolFlags, []string{"--verbose"})
}

func TestResidentLaunchDir_RepairsLooseMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".argus", residentLaunchDirName)
	testutil.NoError(t, os.MkdirAll(dir, 0o755))
	testutil.NoError(t, os.Chmod(dir, 0o755)) //nolint:gosec // deliberately loose mode under test
	got, err := residentLaunchDir()
	testutil.NoError(t, err)
	testutil.Equal(t, got, dir)
	fi, err := os.Lstat(dir)
	testutil.NoError(t, err)
	testutil.Equal(t, fi.Mode().Perm(), os.FileMode(0o700))
}

// A planted symlink whose target already holds the exact expected bytes is
// still replaced by a regular file (content equality must not bless a link).
func TestMaterializeResidentPrompt_SymlinkWithExactBytesReplaced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	file, err := materializeResidentPrompt("", "RULES")
	testutil.NoError(t, err)
	want, err := os.ReadFile(file)
	testutil.NoError(t, err)
	target := filepath.Join(t.TempDir(), "same.md")
	testutil.NoError(t, os.WriteFile(target, want, 0o644))
	testutil.NoError(t, os.Remove(file))
	testutil.NoError(t, os.Symlink(target, file))
	got, err := materializeResidentPrompt("", "RULES")
	testutil.NoError(t, err)
	fi, err := os.Lstat(got)
	testutil.NoError(t, err)
	testutil.True(t, fi.Mode().IsRegular())
	b, _ := os.ReadFile(target)
	testutil.Equal(t, string(b), string(want))
}

func TestEnsureResidentUserSkillsDir_ReplacesNonEmptyDirectory(t *testing.T) {
	task, acct := residentFixture(t)
	testutil.NoError(t, os.MkdirAll(filepath.Join(acct, "skills"), 0o755))
	root := ensureResidentUserSkillsDir(task, residentCfg())
	testutil.True(t, root != "")
	link := filepath.Join(root, ".claude", "skills")
	testutil.NoError(t, os.Remove(link))
	testutil.NoError(t, os.MkdirAll(filepath.Join(link, "evil"), 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(link, "evil", "SKILL.md"), []byte("x"), 0o644))
	got := ensureResidentUserSkillsDir(task, residentCfg())
	testutil.Equal(t, got, root)
	cur, err := os.Readlink(link)
	testutil.NoError(t, err)
	testutil.Equal(t, cur, filepath.Join(acct, "skills"))
}

// Concurrent first launches on a fresh HOME must all succeed (the Lstat->Mkdir
// and Remove->Symlink windows used to fail with "file exists", launching the
// resident silently without CLAUDE.md / skills).
func TestResidentLaunch_ConcurrentFirstLaunches(t *testing.T) {
	task, acct := residentFixture(t)
	testutil.NoError(t, os.MkdirAll(filepath.Join(acct, "skills"), 0o755))
	const n = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	files := make([]string, n)
	roots := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			files[i], errs[i] = materializeResidentPrompt("", "RULES")
			roots[i] = ensureResidentUserSkillsDir(task, residentCfg())
		}()
	}
	close(start)
	wg.Wait()
	for i := 0; i < n; i++ {
		testutil.NoError(t, errs[i])
		testutil.Equal(t, files[i], files[0])
		testutil.True(t, roots[i] != "")
		testutil.Equal(t, roots[i], roots[0])
	}
	b, err := os.ReadFile(files[0])
	testutil.NoError(t, err)
	testutil.Contains(t, string(b), "RULES")
	cur, err := os.Readlink(filepath.Join(roots[0], ".claude", "skills"))
	testutil.NoError(t, err)
	testutil.Equal(t, cur, filepath.Join(acct, "skills"))
}

// Every printable ASCII character outside the allowed classes is refused in
// both the executable and a value position, so widening either class is a
// deliberate test change.
func TestCheckClaudeResidentCommand_EveryUnsafeCharacterRefused(t *testing.T) {
	const exeAllowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./+@%:,-"
	const valueAllowed = exeAllowed + "="
	for c := byte(0x21); c < 0x7f; c++ {
		ch := string(c)
		if !strings.Contains(exeAllowed, ch) {
			err := checkClaudeResidentCommand("/opt/x" + ch + "y/claude")
			if err == nil {
				t.Errorf("executable with %q accepted", ch)
			}
		}
		if !strings.Contains(valueAllowed, ch) {
			if err := checkClaudeResidentCommand("claude --model x" + ch + "y"); err == nil {
				t.Errorf("value with %q accepted", ch)
			}
		}
	}
}

func TestBuildCmd_ResidentRefusesMultilineBackend(t *testing.T) {
	task, _ := residentFixture(t)
	cfg := residentCfg()
	cfg.Backends["claude"] = config.Backend{Command: "claude --model opus\n"}
	_, _, err := BuildCmd(task, cfg, false)
	testutil.Error(t, err)
	testutil.Contains(t, err.Error(), "one line")
}

// TestBuildCmd_ResidentContinuationFlagsReachClaudeUnderSh runs the real
// resident command through sh with a stub `claude` that prints its argv: a
// backslash-newline continuation (leading or trailing) in the backend command
// must still deliver every flag Argus appends as separate arguments. A later
// trim of the command, for example, would turn a trailing `\` + appended
// space into an escaped space and silently drop the forced permission mode.
func TestBuildCmd_ResidentContinuationFlagsReachClaudeUnderSh(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	sandboxAvailabilityForSignalTest(t, false)
	for _, backendCmd := range []string{"claude \\\n --model x", "claude --model x \\\n"} {
		t.Run(strings.ReplaceAll(backendCmd, "\n", `\n`), func(t *testing.T) {
			task, _ := residentFixture(t)
			bin := t.TempDir()
			testutil.NoError(t, os.WriteFile(filepath.Join(bin, "claude"),
				[]byte("#!/bin/sh\nfor a in \"$@\"; do printf '<%s>\\n' \"$a\"; done\n"), 0o755)) //nolint:gosec // test stub must be executable
			cfg := residentCfg()
			cfg.Backends["claude"] = config.Backend{Command: backendCmd}
			cmd := buildResident(t, task, cfg)
			cmd.Env = append(cmd.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			cmd.Stdin = nil
			out, err := cmd.Output()
			testutil.NoError(t, err)
			got := string(out)
			for _, want := range []string{"<--model>", "<x>", "<--permission-mode>", "<default>", "<--setting-sources>", "<project>", "<--settings>"} {
				testutil.Contains(t, got, want+"\n")
			}
			if strings.Contains(got, " --permission-mode") {
				t.Fatalf("appended flags were joined into one argument: %s", got)
			}
		})
	}
}
