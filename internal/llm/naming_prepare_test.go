package llm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestGenerateNamePreparation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success and cleanup", true: "preparation error"}[fail], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			setupFakeClaude(t, `{"type":"item.completed","item":{"type":"agent_message","text":"task-backend-name"}}`, nil)
			testutil.NoError(t, writeExec(filepath.Join(os.Getenv("PATH"), "codex"), "#!/bin/sh\nexit 0\n"))
			sentinel := errors.New("prepare failed")
			root := ""
			utility := NamingBackend{Binary: "codex", Args: CodexNameArgs, JSONEvents: true}
			utility.Prepare = func(b NamingBackend, instructions string) (NamingBackend, func(), error) {
				if fail {
					return b, nil, sentinel
				}
				b, cleanup, err := PrepareCodexNaming(b, instructions)
				for _, entry := range b.Env {
					if strings.HasPrefix(entry, "CODEX_HOME=") {
						root = strings.TrimPrefix(entry, "CODEX_HOME=")
					}
				}
				return b, cleanup, err
			}
			got, err := GenerateNameWithBackend(context.Background(), "some task", utility)
			if fail {
				testutil.ErrorIs(t, err, sentinel)
				return
			}
			testutil.NoError(t, err)
			testutil.Equal(t, got, "task-backend-name")
			testutil.Equal(t, root != "", true)
			_, err = os.Stat(root)
			testutil.Equal(t, os.IsNotExist(err), true)
		})
	}
}

func TestPrepareCodexNamingFilesystemFailures(t *testing.T) {
	t.Run("no temp directory", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		_, _, err := PrepareCodexNaming(NamingBackend{}, "instructions")
		testutil.Equal(t, err != nil, true)
	})
	t.Run("no home directory", func(t *testing.T) {
		t.Setenv("HOME", "")
		_, _, err := PrepareCodexNaming(NamingBackend{}, "instructions")
		testutil.Equal(t, err != nil, true)
	})
	t.Run("invalid auth source", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		file := filepath.Join(t.TempDir(), "file")
		testutil.NoError(t, os.WriteFile(file, []byte("not a directory"), 0600))
		_, _, err := PrepareCodexNaming(NamingBackend{Env: []string{"CODEX_HOME=" + file}}, "instructions")
		testutil.Equal(t, err != nil, true)
	})
	// Existing deadline is preserved by the shared generator (without a live CLI).
	t.Run("caller canceled", func(t *testing.T) {
		setupFakeClaude(t, "unused", nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := GenerateName(ctx, "some task")
		testutil.ErrorIs(t, err, context.Canceled)
	})
}
