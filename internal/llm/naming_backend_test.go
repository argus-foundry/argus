package llm

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCodexNaming(t *testing.T) {
	for _, tc := range []struct{ name, output, want string }{
		{"valid", `{"type":"thread.started","thread_id":"ignore"}\n{"type":"item.completed","item":{"type":"reasoning","text":"wrong-name"}}\n{"type":"item.completed","item":{"type":"agent_message","text":"backend-aware-naming"}}\n{"type":"turn.completed"}`, "backend-aware-naming"},
		{"no message", `{"type":"turn.completed"}`, ""},
		{"malformed", "not json", ""},
		{"invalid name", `{"type":"item.completed","item":{"type":"agent_message","text":"a sentence with spaces"}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var args []string
			setupFakeClaude(t, tc.output, &args)
			// Install a Codex executable; the factory still runs only the hermetic stub.
			testutil.NoError(t, writeExec(strings.Split(os.Getenv("PATH"), ":")[0]+"/codex", "#!/bin/sh\nexit 0\n"))
			prev := nameGenCmd
			nameGenCmd = func(ctx context.Context, binary string, argv ...string) *exec.Cmd {
				testutil.Equal(t, binary, "codex")
				return prev(ctx, binary, argv...)
			}
			name, err := GenerateNameWithBackend(context.Background(), "--model expensive", NamingBackend{Binary: "codex", Args: CodexNameArgs, JSONEvents: true, Env: []string{"NAMING_TEST=1"}})
			testutil.Equal(t, name, tc.want)
			if tc.want == "" {
				testutil.Equal(t, err != nil, true)
			} else {
				testutil.NoError(t, err)
			}
			joined := strings.Join(args, "\n")
			for _, flag := range []string{"exec", "gpt-6-luna", "--ignore-user-config", "--ignore-rules", "--ephemeral", "--skip-git-repo-check", "read-only", "features.shell_tool=false", "project_doc_max_bytes=0", "Task description: --model expensive"} {
				testutil.Contains(t, joined, flag)
			}
			testutil.Equal(t, strings.Contains(joined, "--max-budget-usd"), false)
		})
	}
}

func TestCodexUnavailableDoesNotUseClaude(t *testing.T) {
	setupFakeClaude(t, "should-not-run", nil)
	_, err := GenerateNameWithBackend(context.Background(), "some task", NamingBackend{Binary: "codex", Args: CodexNameArgs})
	testutil.ErrorIs(t, err, ErrUnavailable)
}
