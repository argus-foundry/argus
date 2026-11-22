package config

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestAgentCommitSigningConfig(t *testing.T) {
	for _, tc := range []struct {
		name, body, want, wantKey string
		// wantErr lists substrings the load error must contain; empty = valid.
		wantErr []string
	}{
		{"absent", "", "inherit", "", nil},
		{"inherit", "[git]\nagent_commit_signing = 'inherit'", "inherit", "", nil},
		{"unsigned", "[git]\nagent_commit_signing = 'unsigned'", "unsigned", "", nil},
		{"agent-key with op ref", "[git]\nagent_commit_signing = 'agent-key'\nagent_signing_key = 'op://claude/Argus agent signing key'", "agent-key", "op://claude/Argus agent signing key", nil},
		{"invalid", "[git]\nagent_commit_signing = 'off'", "inherit", "", []string{"git.agent_commit_signing", "inherit, unsigned or agent-key"}},
		{"agent-key missing key", "[git]\nagent_commit_signing = 'agent-key'", "inherit", "", []string{"git.agent_signing_key"}},
		{"agent-key empty op ref", "[git]\nagent_commit_signing = 'agent-key'\nagent_signing_key = 'op://'", "inherit", "", []string{"git.agent_signing_key"}},
		{"agent-key non-op key", "[git]\nagent_commit_signing = 'agent-key'\nagent_signing_key = 'env://SIGNING_KEY'", "inherit", "", []string{"git.agent_signing_key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loader := NewFileLoader(writeFile(t, tc.body))
			base := DefaultConfig()
			got := loader.Apply(base)
			if len(tc.wantErr) > 0 {
				if loader.Err() == nil {
					t.Fatalf("want a load error naming %v, got nil", tc.wantErr)
				}
				for _, want := range tc.wantErr {
					testutil.Contains(t, loader.Err().Error(), want)
				}
			} else {
				testutil.NoError(t, loader.Err())
			}
			testutil.Equal(t, got.Git.AgentCommitSigning, tc.want)
			testutil.Equal(t, got.Git.AgentSigningKey, tc.wantKey)
			testutil.Equal(t, base.Git.AgentCommitSigning, "inherit")
		})
	}
}

// TestGitConfigValidate pins the policy constants and Validate's messages
// directly, independent of the file loader (add-agent-signing-key).
func TestGitConfigValidate(t *testing.T) {
	testutil.Equal(t, GitSigningInherit, "inherit")
	testutil.Equal(t, GitSigningUnsigned, "unsigned")
	testutil.Equal(t, GitSigningAgentKey, "agent-key")
	for _, tc := range []struct {
		name    string
		cfg     GitConfig
		wantErr []string
	}{
		{"zero value", GitConfig{}, nil},
		{"inherit", GitConfig{AgentCommitSigning: GitSigningInherit}, nil},
		{"unsigned", GitConfig{AgentCommitSigning: GitSigningUnsigned}, nil},
		{"agent-key op ref", GitConfig{AgentCommitSigning: GitSigningAgentKey, AgentSigningKey: "op://claude/Argus agent signing key"}, nil},
		// A key reference alongside a non-agent-key policy is unused, not an error.
		{"unsigned with stray key", GitConfig{AgentCommitSigning: GitSigningUnsigned, AgentSigningKey: "op://claude/k"}, nil},
		{"unknown policy", GitConfig{AgentCommitSigning: "sign"}, []string{"git.agent_commit_signing", "inherit, unsigned or agent-key"}},
		{"agent-key empty key", GitConfig{AgentCommitSigning: GitSigningAgentKey}, []string{"git.agent_signing_key"}},
		{"agent-key blank key", GitConfig{AgentCommitSigning: GitSigningAgentKey, AgentSigningKey: "   "}, []string{"git.agent_signing_key"}},
		{"agent-key env ref", GitConfig{AgentCommitSigning: GitSigningAgentKey, AgentSigningKey: "env://K"}, []string{"git.agent_signing_key"}},
		{"agent-key bare scheme", GitConfig{AgentCommitSigning: GitSigningAgentKey, AgentSigningKey: "op://"}, []string{"git.agent_signing_key"}},
		{"agent-key bare path", GitConfig{AgentCommitSigning: GitSigningAgentKey, AgentSigningKey: "claude/Argus agent signing key"}, []string{"git.agent_signing_key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if len(tc.wantErr) == 0 {
				testutil.NoError(t, err)
				return
			}
			if err == nil {
				t.Fatalf("want an error naming %v, got nil", tc.wantErr)
			}
			for _, want := range tc.wantErr {
				testutil.Contains(t, err.Error(), want)
			}
		})
	}
}
