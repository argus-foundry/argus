package agent

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const autoVerdictStop = "Auto mode is unavailable — the server returned no safety verdict for the last 10 responses, so Claude stopped. Send a message to try again, or switch out of auto mode."

func TestBuildCmd_ResidentAutoOnResume(t *testing.T) {
	for _, mode := range []string{config.PermissionModeDefault, config.PermissionModeBypassActive} {
		t.Run(mode, func(t *testing.T) {
			task, _ := residentFixture(t)
			task.SessionID = "12345678-1234-1234-1234-123456789abc"
			cfg := residentCfg()
			cfg.Defaults.PermissionMode = mode
			cfg.Backends["claude"] = config.Backend{Command: "claude --permission-mode=bypassPermissions --dangerously-skip-permissions"}
			cmd, cleanup, err := BuildCmd(task, cfg, true)
			testutil.NoError(t, err)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}
			line := strings.Join(cmd.Args, " ")
			testutil.Contains(t, line, "--permission-mode auto")
			testutil.Contains(t, line, "--resume "+shellQuote(task.SessionID))
			testutil.False(t, strings.Contains(line, "skip-permissions"))
			st, ok := settingsArg(t, cmd.Args)
			testutil.True(t, ok)
			perms := st["permissions"].(map[string]any)
			testutil.Equal(t, perms["defaultMode"], any("auto"))
			testutil.Equal(t, perms["disableBypassPermissionsMode"], any("disable"))
		})
	}
}

func TestBuildCmd_ResidentReadOnlyBashGrants(t *testing.T) {
	task, _ := residentFixture(t)
	settings, ok := settingsArg(t, buildResident(t, task, residentCfg()).Args)
	testutil.True(t, ok)
	perms := settings["permissions"].(map[string]any)
	var shellAllows []string
	for _, raw := range perms["allow"].([]any) {
		rule := raw.(string)
		if rule == "Bash" || strings.HasPrefix(rule, "Bash(") {
			shellAllows = append(shellAllows, rule)
		}
	}
	// Pin the entire delivered shell tier: an added wildcard, gh/push grant,
	// find -exec/-delete, rg --pre or branch mutation allowance fails here.
	testutil.DeepEqual(t, shellAllows, []string{
		"Bash(ls:*)", "Bash(cat:*)", "Bash(head:*)", "Bash(tail:*)",
		"Bash(grep:*)", "Bash(wc:*)", "Bash(git status:*)",
		"Bash(rg --files)", "Bash(find . -type f)",
		"Bash(git log)", "Bash(git diff)", "Bash(git show)", "Bash(git branch)",
	})
}

func TestAutoModeNeedsInput(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       bool
	}{
		{"terminal stop", autoVerdictStop + "\n❯\u00a0", true},
		{"wrapped stop", strings.ReplaceAll(autoVerdictStop, "Send a message", "\nSend a message") + "\n❯\u00a0", true},
		{"wrapped signature", strings.NewReplacer("returned no", "returned\nno", "auto mode.", "auto\nmode.").Replace(autoVerdictStop) + "\n✻ Worked for 1s\n❯\u00a0", true},
		{"recovered", autoVerdictStop + "\nCompleted the task.\n❯\u00a0", false},
		{"still busy", autoVerdictStop + "\nesc to interrupt\n❯\u00a0", false},
		{"transient retry", "Auto mode check unavailable\nNot run · auto mode's check had no usable answer\n❯\u00a0", false},
		{"ordinary block", "Auto mode blocked this action.\nTrying another approach.\n❯\u00a0", false},
		{"context escalation", "Auto mode classifier transcript exceeded context window — falling back to manual approval\n❯ 1. Yes\n  2. No", true},
		{"repeated block escalation", "Auto mode paused after repeated blocks\n  1. Yes\n❯ 2. No", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tail := []byte(tc.text)
			testutil.Equal(t, DetectNeedsInput(tail), tc.want)
			_, awaiting := AwaitingInputFingerprint(&ScreenRenderer{}, tail, 200, 24)
			testutil.Equal(t, awaiting, tc.want)
			view := NewTailView(&ScreenRenderer{}, tail, 200, 24)
			testutil.Equal(t, view.DetectNeedsInputScreen(), tc.want)
			if tc.name == "terminal stop" || tc.name == "wrapped signature" {
				testutil.True(t, view.AutoModeStopped())
			}
		})
	}
}

func TestAutoModeStoppedEmptyAndRendered(t *testing.T) {
	testutil.False(t, NewTailView(nil, nil, 80, 24).AutoModeStopped())
	testutil.False(t, NewTailView(nil, []byte("ordinary output"), 80, 24).AutoModeStopped())
	// A repainted screen may split every glyph with cursor-control codes;
	// exercise the rendered fallback rather than matching a contiguous raw row.
	var raw strings.Builder
	raw.WriteString("\x1b[2J\x1b[H")
	chars := []rune(autoVerdictStop)
	for col := len(chars) - 1; col >= 0; col-- {
		fmt.Fprintf(&raw, "\x1b[%dG%c", col+1, chars[col])
	}
	raw.WriteString("\r\n❯\u00a0")
	testutil.False(t, NewTailView(nil, []byte(raw.String()), 240, 24).AutoModeStopped())
	testutil.True(t, NewTailView(&ScreenRenderer{}, []byte(raw.String()), 240, 24).AutoModeStopped())
}

func TestResidentAutoFallbackNotices(t *testing.T) {
	for _, notice := range []string{
		"auto mode disabled by settings", "auto mode is unavailable for your plan",
		"auto mode unavailable for this model", "auto mode unavailable while fast mode is on · run /fast off",
		"auto mode is unavailable right now",
	} {
		t.Run(notice, func(t *testing.T) {
			var logs syncBuf
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })
			watch := residentAutoFallback{enabled: true}
			// Split inside both ANSI and the notice, as real PTY reads can.
			raw := "\x1b[33m" + notice + "\x1b[0m"
			for _, b := range []byte(raw) {
				watch.observe("solo", []byte{b})
			}
			watch.observe("solo", []byte(raw))
			testutil.Equal(t, strings.Count(logs.String(), "[resident] auto permission fallback"), 1)
			testutil.Contains(t, logs.String(), "mode=default")
		})
	}
}

func TestResidentAutoFallbackIgnoresOtherOutput(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		text    string
	}{
		{false, "auto mode is unavailable for your plan"},
		{true, "manual mode on"},
		{true, autoVerdictStop},
		{true, "Auto mode check unavailable"},
		{true, "Auto mode blocked this action"},
		{true, strings.Repeat("x", 10000)},
	} {
		t.Run(tc.text[:min(len(tc.text), 40)], func(t *testing.T) {
			watch := residentAutoFallback{enabled: tc.enabled}
			watch.observe("solo", []byte(tc.text))
			testutil.False(t, watch.logged)
			testutil.True(t, len(watch.tail) <= 4096)
		})
	}
}

func TestResidentAutoFallbackAcrossWindow(t *testing.T) {
	watch := residentAutoFallback{enabled: true}
	const first = "auto mode is un"
	const last = "available for your plan"
	watch.observe("solo", []byte(strings.Repeat("x", 4096-len(first))+first))
	watch.observe("solo", []byte(last+strings.Repeat("x", 4096-len(last))))
	testutil.True(t, watch.logged)
}

func TestResidentAutoFallbackFromPTY(t *testing.T) {
	for _, resident := range []bool{false, true} {
		t.Run(fmt.Sprintf("resident=%t", resident), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			var logs syncBuf
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })
			logPath := filepath.Join(t.TempDir(), "ux.log")
			testutil.NoError(t, uxlog.Init(logPath))
			t.Cleanup(uxlog.Close)
			cmd := exec.Command("printf", "%s\n", "auto mode is unavailable for your plan", "Manual session ready")
			cmd.Env = os.Environ()
			if resident {
				cmd.Env = append(cmd.Env, ResidentEnvMarker+"=1")
			}
			sess, err := StartSession("solo", cmd, 24, 80)
			testutil.NoError(t, err)
			select {
			case <-sess.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("fixture did not exit")
			}
			testutil.Contains(t, string(sess.RecentOutput()), "Manual session ready")
			testutil.Equal(t, strings.Contains(logs.String(), "[resident] auto permission fallback"), resident)
			uxBytes, err := os.ReadFile(logPath)
			testutil.NoError(t, err)
			testutil.Equal(t, strings.Contains(string(uxBytes), "[resident] auto permission fallback"), resident)
		})
	}
}
