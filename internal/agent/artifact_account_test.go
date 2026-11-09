package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/backendkind"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// artifactCfg is a config with a "personal" Claude account, a project whose
// artifact home account is personal ("history"), and an unmapped project.
func artifactCfg(personalDir string) config.Config {
	cfg := permModeConfig("")
	cfg.Accounts = map[string]config.Account{"personal": {ClaudeConfigDir: personalDir}}
	cfg.ProjectArtifactAccounts = map[string]string{"history": "personal"}
	return cfg
}

func buildArtifactEnv(t *testing.T, cfg config.Config, task *model.Task) []string {
	t.Helper()
	cmd, cleanup, err := BuildCmd(task, cfg, false)
	testutil.NoError(t, err)
	if cleanup != nil {
		cleanup()
	}
	if cmd == nil {
		t.Fatal("BuildCmd returned a nil cmd")
	}
	return cmd.Env
}

func TestBuildCmd_ArtifactAccountExport(t *testing.T) {
	seedDefaultClaude(t)
	personalDir := filepath.Join(t.TempDir(), "personal")
	// An inherited value from the parent process must never leak through.
	t.Setenv("ARGUS_ARTIFACT_ACCOUNT", "inherited")

	tests := []struct {
		name    string
		home    string // the "history" project's artifact entry; "" = personal
		backend string
		project string
		account string
		want    string // "" means the variable must be absent
	}{
		{name: "claude task on its home account", project: "history", account: "personal"},
		{name: "claude task routed to default account", project: "history", account: "default", want: "personal"},
		{name: "claude task with blank account is on default", project: "history", account: "", want: "personal"},
		{name: "codex task in mapped project", backend: "codex", project: "history", want: "personal"},
		{name: "pi task in mapped project", backend: "pi", project: "history", want: "personal"},
		// The !NativeArtifacts branch on its own: the task runs ON its home
		// account, so only the missing built-in tool forces the export.
		{name: "codex task on its default home account", home: "default", backend: "codex", project: "history", account: "", want: "default"},
		{name: "pi task on its default home account", home: "default", backend: "pi", project: "history", account: "default", want: "default"},
		// Blank-account normalization against a "default" home.
		{name: "claude blank account on default home", home: "default", project: "history", account: ""},
		{name: "claude default account on default home", home: "default", project: "history", account: "default"},
		{name: "claude task in unmapped project", project: "other", account: "default"},
		{name: "claude task in unmapped project on named account", project: "other", account: "personal"},
		{name: "codex task in unmapped project", backend: "codex", project: "other"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := artifactCfg(personalDir)
			if tc.home != "" {
				cfg.ProjectArtifactAccounts = map[string]string{"history": tc.home}
			}
			task := &model.Task{
				ID: "t1", Name: "t", Prompt: "go", Worktree: t.TempDir(),
				Backend: tc.backend, Project: tc.project, Account: tc.account,
			}
			got, ok := envValue(buildArtifactEnv(t, cfg, task), "ARGUS_ARTIFACT_ACCOUNT")
			if tc.want == "" {
				testutil.False(t, ok)
				return
			}
			testutil.True(t, ok)
			testutil.Equal(t, got, tc.want)
		})
	}

	t.Run("falls back to project_accounts home", func(t *testing.T) {
		cfg := artifactCfg(personalDir)
		cfg.ProjectArtifactAccounts = nil
		cfg.ProjectAccounts = map[string]string{"history": "personal"}
		task := &model.Task{ID: "t1", Name: "t", Prompt: "go", Worktree: t.TempDir(), Project: "history", Account: "default"}
		got, ok := envValue(buildArtifactEnv(t, cfg, task), "ARGUS_ARTIFACT_ACCOUNT")
		testutil.True(t, ok)
		testutil.Equal(t, got, "personal")
	})

	t.Run("account move back home recomputes the export", func(t *testing.T) {
		cfg := artifactCfg(personalDir)
		task := &model.Task{ID: "t1", Name: "t", SessionID: "sid", Worktree: t.TempDir(), Project: "history", Account: "default"}
		got, ok := envValue(buildArtifactEnv(t, cfg, task), "ARGUS_ARTIFACT_ACCOUNT")
		testutil.True(t, ok)
		testutil.Equal(t, got, "personal")

		task.Account = "personal" // the move persists the new account, then restarts
		cmd, cleanup, err := BuildCmd(task, cfg, true)
		testutil.NoError(t, err)
		if cleanup != nil {
			cleanup()
		}
		_, ok = envValue(cmd.Env, "ARGUS_ARTIFACT_ACCOUNT")
		testutil.False(t, ok)
	})
}

func TestSpawnBaseEnv_StripsArtifactAccountUnderArtifactsPrefix(t *testing.T) {
	logs := captureUXLog(t)
	got := spawnBaseEnv([]string{"PATH=/bin", "ARGUS_ARTIFACT_ACCOUNT=inherited", "CLAUDE_CONFIG_DIR=/x"}, nil, "t1")
	testutil.DeepEqual(t, got, []string{"PATH=/bin"})
	out := logs()
	testutil.Contains(t, out, `[artifacts] task "t1": stripped inherited ARGUS_ARTIFACT_ACCOUNT`)
	testutil.Contains(t, out, `[account] task "t1": stripped inherited env CLAUDE_CONFIG_DIR`)
	testutil.False(t, strings.Contains(out, "env ARGUS_ARTIFACT_ACCOUNT"))
}

func TestFamilyNativeArtifactsCapability(t *testing.T) {
	// Walk the real registry so a new family can't silently claim the tool.
	for _, fam := range append(append([]*Family(nil), families...), customFamily) {
		name := string(fam.Kind)
		if name == "" {
			name = "custom"
		}
		t.Run(name, func(t *testing.T) {
			testutil.Equal(t, fam.NativeArtifacts, fam.Kind == backendkind.Claude)
		})
	}
	t.Run("unknown kind resolves to custom", func(t *testing.T) {
		fam := FamilyForKind(backendkind.Kind("no-such-cli"))
		testutil.True(t, fam == customFamily)
		testutil.False(t, fam.NativeArtifacts)
	})
}
