package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/llm"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestNamingBackend(t *testing.T) {
	for _, tc := range []struct{ command, binary string }{
		{"claude --model opus", "claude"}, {"codex --model gpt-6-astra", "codex"},
		{"pi", ""}, {"opencode", ""}, {"agy", ""}, {"cursor-agent", ""}, {"omp", ""}, {"custom-agent", ""},
	} {
		t.Run(tc.command, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CODEX_HOME", "/wrong-account")
			t.Setenv("CLAUDE_CONFIG_DIR", "/wrong-account")
			t.Setenv("ARGUS_TASK_ID", "parent-task")
			cfg := config.Config{Backends: map[string]config.Backend{"chosen": {Command: tc.command}}}
			cfg.Defaults.Backend = "claude"
			utility, err := namingBackend(model.Task{Backend: "chosen", Model: "expensive"}, cfg)
			if tc.binary == "" {
				testutil.ErrorIs(t, err, llm.ErrUnavailable)
				return
			}
			testutil.NoError(t, err)
			testutil.Equal(t, utility.Binary, tc.binary)
			env := strings.Join(utility.Env, "\n")
			testutil.Equal(t, strings.Contains(env, "/wrong-account"), false)
			testutil.Equal(t, strings.Contains(env, "ARGUS_TASK_ID="), false)
		})
	}
}

func TestNamingAccountAndCredentials(t *testing.T) {
	for _, command := range []string{"claude", "codex"} {
		t.Run(command, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("OPENAI_API_KEY", "wrong-secret")
			t.Setenv("ANTHROPIC_API_KEY", "wrong-secret")
			dir := filepath.Join(home, "account")
			cfg := config.Config{
				Backends: map[string]config.Backend{"chosen": {Command: command, EnvVars: map[string]string{"NAMING_KEY": "env://NAMING_SOURCE"}}},
				Accounts: map[string]config.Account{"work": {ClaudeConfigDir: dir, CodexHome: dir, Inherit: []string{}}},
			}
			t.Setenv("NAMING_SOURCE", "right-secret")
			utility, err := namingBackend(model.Task{Backend: "chosen", Account: "work"}, cfg)
			testutil.NoError(t, err)
			env := strings.Join(utility.Env, "\n")
			testutil.Contains(t, env, "NAMING_KEY=right-secret")
			if command == "codex" {
				testutil.Contains(t, env, "CODEX_HOME="+dir)
				testutil.Equal(t, strings.Contains(env, "OPENAI_API_KEY=wrong-secret"), false)
			} else {
				testutil.Contains(t, env, "CLAUDE_CONFIG_DIR="+dir)
				testutil.Equal(t, strings.Contains(env, "ANTHROPIC_API_KEY=wrong-secret"), false)
			}
			_, err = namingBackend(model.Task{Backend: "chosen", Account: "removed"}, cfg)
			testutil.Equal(t, err != nil, true)
			testutil.NoError(t, os.Unsetenv("NAMING_SOURCE"))
			_, err = namingBackend(model.Task{Backend: "chosen", Account: "work"}, cfg)
			testutil.Equal(t, err != nil, true)
		})
	}
	_, err := namingBackend(model.Task{Backend: "missing"}, config.Config{})
	testutil.Equal(t, err != nil, true)
}
