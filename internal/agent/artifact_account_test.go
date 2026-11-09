package agent

import (
	"path/filepath"
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
		{name: "claude task in unmapped project", project: "other", account: "default"},
		{name: "claude task in unmapped project on named account", project: "other", account: "personal"},
		{name: "codex task in unmapped project", backend: "codex", project: "other"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{
				ID: "t1", Name: "t", Prompt: "go", Worktree: t.TempDir(),
				Backend: tc.backend, Project: tc.project, Account: tc.account,
			}
			got, ok := envValue(buildArtifactEnv(t, artifactCfg(personalDir), task), "ARGUS_ARTIFACT_ACCOUNT")
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

func TestFamilyNativeArtifactsCapability(t *testing.T) {
	for _, tt := range []struct {
		kind backendkind.Kind
		want bool
	}{
		{backendkind.Claude, true}, {backendkind.Codex, false}, {backendkind.Pi, false},
		{backendkind.Opencode, false}, {backendkind.Antigravity, false}, {backendkind.Cursor, false},
		{backendkind.Omp, false},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			testutil.Equal(t, FamilyForKind(tt.kind).NativeArtifacts, tt.want)
		})
	}
	t.Run("custom", func(t *testing.T) {
		fam := FamilyForKind(backendkind.Kind("no-such-cli"))
		testutil.True(t, fam == customFamily)
		testutil.False(t, fam.NativeArtifacts)
	})
}
