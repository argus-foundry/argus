package config

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

// captureSlog swaps the default slog logger for one writing to a buffer for
// the duration of the test.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestArtifactAccount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Config{
		Accounts: map[string]Account{
			"personal": {ClaudeConfigDir: "~/.claude-personal"},
			"work":     {ClaudeConfigDir: "/opt/claude-work"},
		},
		DefaultAccount: "personal",
		ProjectAccounts: map[string]string{
			"both":           "default",
			"project-only":   "personal",
			"unknown-art":    "personal",
			"bad-project":    "nope",
			"both-unknown":   "gone",
			"artifact-blank": "work",
		},
		ProjectArtifactAccounts: map[string]string{
			"both":           "personal",
			"unknown-art":    "nope",
			"artifact-only":  "work",
			"default-home":   "default",
			"both-unknown":   "missing",
			"artifact-blank": "",
		},
	}
	tests := []struct {
		name    string
		project string
		want    string
		wantLog string
	}{
		{name: "explicit artifact entry wins", project: "both", want: "personal"},
		{name: "falls back to project account entry", project: "project-only", want: "personal"},
		{name: "artifact entry alone", project: "artifact-only", want: "work"},
		{name: "default is a valid home account", project: "default-home", want: "default"},
		{name: "unmapped project has none despite default_account", project: "unmapped", want: ""},
		{name: "empty project has none", project: "", want: ""},
		{name: "unknown artifact entry skipped, falls back", project: "unknown-art", want: "personal", wantLog: "nope"},
		{name: "unknown project entry skipped", project: "bad-project", want: "", wantLog: "nope"},
		{name: "both unknown resolves none", project: "both-unknown", want: "", wantLog: "missing"},
		{name: "blank artifact entry falls back", project: "artifact-blank", want: "work"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureSlog(t)
			testutil.Equal(t, cfg.ArtifactAccount(tc.project), tc.want)
			if tc.wantLog != "" {
				testutil.Contains(t, logs.String(), "[artifacts] skipping unknown artifact account")
				testutil.Contains(t, logs.String(), tc.wantLog)
			} else {
				testutil.Equal(t, logs.String(), "")
			}
		})
	}
}

func TestProjectArtifactAccounts_ParseAndMerge(t *testing.T) {
	path := writeFile(t, `
[project_accounts]
argus = "work"

[project_artifact_accounts]
history = "personal"
argus = "personal"
`)
	base := Config{
		ProjectAccounts:         map[string]string{"base-proj": "work"},
		ProjectArtifactAccounts: map[string]string{"base-art": "work"},
	}
	l := NewFileLoader(path)
	got := l.Apply(base)
	testutil.NoError(t, l.Err())

	t.Run("decodes the table", func(t *testing.T) {
		testutil.Equal(t, got.ProjectArtifactAccounts["history"], "personal")
		testutil.Equal(t, got.ProjectArtifactAccounts["argus"], "personal")
	})
	t.Run("merges over base entries like project_accounts", func(t *testing.T) {
		testutil.Equal(t, got.ProjectArtifactAccounts["base-art"], "work")
		testutil.Equal(t, got.ProjectAccounts["base-proj"], "work")
		testutil.Equal(t, got.ProjectAccounts["argus"], "work")
	})
	t.Run("base map is not mutated", func(t *testing.T) {
		testutil.Equal(t, len(base.ProjectArtifactAccounts), 1)
		_, ok := base.ProjectArtifactAccounts["history"]
		testutil.False(t, ok)
	})
}
