package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBuildCmd_WorktreeTrust(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		resume, disabled, other bool
	}{
		{name: "fresh"}, {name: "resume", resume: true},
		{name: "disabled", disabled: true}, {name: "unrelated family", other: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			wt := filepath.Join(t.TempDir(), "task.with dots '\" $HOME $(touch PWNED) `touch PWNED2` \\ \nnext")
			testutil.NoError(t, os.Mkdir(wt, 0o700))
			argvPath := filepath.Join(t.TempDir(), "argv")
			t.Setenv("ARGUS_TEST_TRUST_ARGV", argvPath)
			name := "codex"
			if tc.other {
				name = "other-agent"
			}
			bin := filepath.Join(t.TempDir(), name)
			testutil.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\0' \"$@\" > \"$ARGUS_TEST_TRUST_ARGV\"\n"), 0o700))
			t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
			accountConfig := filepath.Join(os.Getenv("HOME"), ".codex", "config.toml")
			testutil.NoError(t, os.MkdirAll(filepath.Dir(accountConfig), 0o700))
			original := "# keep account settings\n[projects.\"/already\"]\ntrust_level = \"trusted\"\n"
			testutil.NoError(t, os.WriteFile(accountConfig, []byte(original), 0o600))
			cfg := testConfig()
			cfg.Agents.TrustWorktrees = !tc.disabled
			cfg.Backends["alias"] = config.Backend{Command: bin}
			task := &model.Task{Backend: "alias", Worktree: wt, Prompt: "hello", SessionID: "session-id"}
			cmd, cleanup, err := BuildCmd(task, cfg, tc.resume)
			testutil.NoError(t, err)
			if cleanup != nil {
				defer cleanup()
			}
			testutil.NoError(t, cmd.Run())
			raw, err := os.ReadFile(argvPath)
			testutil.NoError(t, err)
			args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
			trustArg := ""
			trustArgs := 0
			for i, arg := range args {
				if arg == "-c" && i+1 < len(args) && strings.HasPrefix(args[i+1], "projects=") {
					trustArg = args[i+1]
					trustArgs++
				}
			}
			if tc.disabled || tc.other {
				testutil.Equal(t, trustArgs, 0)
				testutil.Equal(t, trustArg, "")
			} else {
				testutil.Equal(t, trustArgs, 1)
				testutil.True(t, strings.HasPrefix(trustArg, "projects={"))
				var data struct {
					Projects map[string]struct {
						Trust string `toml:"trust_level"`
					} `toml:"projects"`
				}
				_, err := toml.Decode(trustArg, &data)
				testutil.NoError(t, err)
				physical, err := filepath.EvalSymlinks(wt)
				testutil.NoError(t, err)
				testutil.Equal(t, len(data.Projects), 1)
				testutil.Equal(t, data.Projects[physical].Trust, "trusted")
				if tc.resume {
					testutil.Equal(t, args[len(args)-1], "session-id")
				} else {
					testutil.Equal(t, args[len(args)-1], "hello")
				}
			}
			for _, marker := range []string{"PWNED", "PWNED2"} {
				_, err := os.Stat(filepath.Join(wt, marker))
				testutil.True(t, os.IsNotExist(err))
			}
			after, err := os.ReadFile(accountConfig)
			testutil.NoError(t, err)
			testutil.Equal(t, string(after), original)
		})
	}
}

func TestCodexWorktreeTrustFlag_MissingPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wt := filepath.Join(t.TempDir(), "missing")
	_, err := codexWorktreeTrustFlag(wt)
	testutil.NotNil(t, err)
	_, err = codexWorktreeTrustFlag("")
	testutil.NotNil(t, err)
}

func TestBuildCmd_WorktreeTrustHookFailsOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	family := FamilyOf("codex")
	original := family.WorktreeTrustFlag
	family.WorktreeTrustFlag = func(string) (string, error) { return " --bad-trust-flag", errors.New("test trust failure") }
	t.Cleanup(func() { family.WorktreeTrustFlag = original })
	cfg := testConfig()
	cfg.Agents.TrustWorktrees = true
	cmd, cleanup, err := BuildCmd(&model.Task{Backend: "codex", Worktree: t.TempDir()}, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		defer cleanup()
	}
	testutil.Equal(t, strings.Contains(cmd.Args[2], "projects="), false)
	testutil.Equal(t, strings.Contains(cmd.Args[2], "--bad-trust-flag"), false)
}
