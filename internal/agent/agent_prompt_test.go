package agent

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBuildCmd_PiPromptArguments(t *testing.T) {
	for _, tc := range []struct {
		name, prompt, want string
	}{
		{"frontmatter", "---title: \"Investigate Factory deployment failures via Keystone MCP\"tags: [handoff]---## Handoff", "\n---title: \"Investigate Factory deployment failures via Keystone MCP\"tags: [handoff]---## Handoff"},
		{"long_option", "--title \"example\"", "\n--title \"example\""},
		{"short_option", "-x", "\n-x"},
		{"single_dash", "-", "\n-"},
		{"shell_literals", "---\ntitle: 'literal'\n$(exit 42); `exit 43`\n---", "\n---\ntitle: 'literal'\n$(exit 42); `exit 43`\n---"},
		{"ordinary", "fix the bug", "fix the bug"},
		{"existing_whitespace", "\n---title: example", "\n---title: example"},
		{"file_include", "@instructions.md", "@instructions.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			logs := captureUXLog(t)
			task := &model.Task{Backend: "pi", Prompt: tc.prompt, Worktree: t.TempDir()}
			cmd, _, err := BuildCmd(task, testConfig(), false)
			testutil.NoError(t, err)
			// Run only a shell function, never the real Pi CLI. Capture argv after
			// shell parsing to catch quoting failures and accidental extra args.
			probe := exec.Command("bash", "-c", "pi() { printf '%s\\0' \"$@\"; }; "+cmd.Args[2])
			probe.Dir, probe.Env = cmd.Dir, cmd.Env
			out, err := probe.Output()
			testutil.NoError(t, err)
			testutil.Equal(t, string(out), tc.want+"\x00")
			testutil.Equal(t, task.Prompt, tc.prompt)
			testutil.Equal(t, strings.Contains(logs(), "[pi] prefixed newline to dash-leading positional prompt"), strings.HasPrefix(tc.prompt, "-"))
		})
	}
}

func TestBuildCmd_DashPromptDeliveryIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, backend, flag, want string
		resume                    bool
	}{
		{"explicit_flag", "pi", "--prompt", "pi --prompt '---title: example'", false},
		{"other_family", "bare", "", "my-agent -- '---title: example'", false},
		{"resume", "pi", "", "pi --session 'session-123'", true},
		{"empty", "pi", "", "pi", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			cfg := testConfig()
			backend := cfg.Backends[tc.backend]
			backend.PromptFlag = tc.flag
			cfg.Backends[tc.backend] = backend
			prompt := "---title: example"
			if tc.name == "empty" {
				prompt = ""
			}
			task := &model.Task{Backend: tc.backend, Prompt: prompt, SessionID: "session-123", Worktree: t.TempDir()}
			if tc.backend == "bare" {
				task.SessionID = ""
			}
			cmd, _, err := BuildCmd(task, cfg, tc.resume)
			testutil.NoError(t, err)
			testutil.Equal(t, strings.TrimSpace(cmd.Args[2]), tc.want)
		})
	}
}
