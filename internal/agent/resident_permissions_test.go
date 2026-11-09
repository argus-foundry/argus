package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
		{"allowedTools single value", `claude --allowedTools "Bash(git *)" -p`, []string{"claude", "-p"}},
		{"allowedTools variadic list", `claude --allowedTools Read 'Bash(ls *)' Edit --model x`, []string{"claude", "--model", "x"}},
		{"allowed-tools quoted flag", `claude "--allowed-tools" Read -p`, []string{"claude", "-p"}},
		{"allowedTools equals form", `claude --allowedTools="Bash(*)" -p`, []string{"claude", "-p"}},
		{"allowed-tools equals form", `claude --allowed-tools=Read,Edit -p`, []string{"claude", "-p"}},
		{"allowedTools last word", `claude -p --allowedTools`, []string{"claude", "-p"}},
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
func TestBuildCmd_ResidentForcedModeSurvivesQuotedMention(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.NoError(t, os.MkdirAll(db.ResidentWorkDir(), 0o755))
	cfg := residentCfg()
	cfg.Backends["claude"] = config.Backend{Command: `claude --append-system-prompt "never use --permission-mode bypassPermissions"`}
	task := &model.Task{ID: "r", Name: "resident", Kind: model.KindResident, Worktree: db.ResidentWorkDir()}
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	testutil.Contains(t, strings.Join(cmd.Args, " "), "--permission-mode default")
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
		"sandbox": {"enabled": true, "autoAllowBashIfSandboxed": true, "excludedCommands": ["git"], "network": {"allowedDomains": ["example.com"]}},
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
	testutil.Contains(t, line, "example.com")
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
	us := readResidentUserSettings(task, residentCfg())
	testutil.Equal(t, string(us.Sandbox), `{"allowUnsandboxedCommands":false,"autoAllowBashIfSandboxed":false,"enabled":true}`)
	testutil.DeepEqual(t, us.SkippedHooks, []string{"PermissionRequest", "PreToolUse", "Stop"})
	// A non-object sandbox value is ignored rather than injected.
	testutil.NoError(t, os.WriteFile(filepath.Join(acct, "settings.json"), []byte(`{"sandbox": true}`), 0o644))
	us = readResidentUserSettings(task, residentCfg())
	testutil.Equal(t, len(us.Sandbox), 0)
	testutil.Equal(t, len(us.SkippedHooks), 0)
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
	if strings.Contains(line, "resident-skills") || strings.Contains(line, "resident-prompt") {
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

// The content-addressed prompt dir is writable by sandboxed agents, so a
// tampered file must be restored (atomically, replacing even a symlink).
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
	testutil.Equal(t, strings.Contains(pline, "resident-prompt"), false)
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
