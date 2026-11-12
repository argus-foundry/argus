package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestPrepareCodexNaming(t *testing.T) {
	for _, auth := range []bool{false, true} {
		t.Run(map[bool]string{false: "no auth file", true: "account auth"}[auth], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			source := filepath.Join(home, "account")
			testutil.NoError(t, os.Mkdir(source, 0700))
			if auth {
				testutil.NoError(t, os.WriteFile(filepath.Join(source, "auth.json"), []byte("fake-login"), 0600))
			}
			backend, cleanup, err := PrepareCodexNaming(NamingBackend{Binary: "codex", Args: CodexNameArgs, Env: []string{"CODEX_HOME=" + source, "OTHER=kept"}}, "naming instructions")
			testutil.NoError(t, err)
			t.Cleanup(cleanup)
			var root string
			for _, entry := range backend.Env {
				if strings.HasPrefix(entry, "CODEX_HOME=") {
					root = strings.TrimPrefix(entry, "CODEX_HOME=")
				}
			}
			testutil.Equal(t, root != source && root != "", true)
			testutil.Contains(t, strings.Join(backend.Env, "\n"), "OTHER=kept")
			data, err := os.ReadFile(filepath.Join(root, "instructions.md"))
			testutil.NoError(t, err)
			testutil.Equal(t, string(data), "naming instructions")
			if auth {
				target, err := os.Readlink(filepath.Join(root, "auth.json"))
				testutil.NoError(t, err)
				testutil.Equal(t, target, filepath.Join(source, "auth.json"))
			}
			for _, name := range []string{"config.toml", "skills", "AGENTS.md"} {
				_, err := os.Stat(filepath.Join(root, name))
				testutil.Equal(t, os.IsNotExist(err), true)
			}
			args := backend.Args("some task", "naming instructions")
			testutil.Contains(t, strings.Join(args, "\n"), "model_instructions_file=")
			testutil.Equal(t, args[len(args)-2], "--")
			testutil.Equal(t, args[len(args)-1], "Task description: some task")
			cleanup()
			_, err = os.Stat(root)
			testutil.Equal(t, os.IsNotExist(err), true)
		})
	}
}
