package config

import (
	"fmt"
	"strings"
)

// Agent commit-signing policies for GitConfig.AgentCommitSigning.
const (
	GitSigningInherit  = "inherit"
	GitSigningUnsigned = "unsigned"
	GitSigningAgentKey = "agent-key"
)

// GitConfig controls Git defaults in agent child processes only. These are
// config.toml-only settings; host Git and shared repository config are untouched.
type GitConfig struct {
	AgentCommitSigning string `toml:"agent_commit_signing"`
	// AgentSigningKey is the op:// reference to a 1Password SSH Key item
	// (e.g. "op://claude/Argus agent signing key"). Used only by agent-key;
	// ignored for every other policy.
	AgentSigningKey string `toml:"agent_signing_key"`
}

// Validate rejects a misspelled policy instead of silently invoking a signer.
// The zero value is equivalent to inherit for programmatically built configs.
func (g GitConfig) Validate() error {
	switch g.AgentCommitSigning {
	case "", GitSigningInherit, GitSigningUnsigned:
		return nil
	case GitSigningAgentKey:
		key := strings.TrimSpace(g.AgentSigningKey)
		if key == "" {
			return fmt.Errorf("git.agent_signing_key is required when git.agent_commit_signing is agent-key")
		}
		if !strings.HasPrefix(key, "op://") || len(key) == len("op://") {
			return fmt.Errorf("git.agent_signing_key must be an op:// reference to a 1Password SSH Key item")
		}
		return nil
	default:
		return fmt.Errorf("git.agent_commit_signing must be inherit, unsigned or agent-key")
	}
}
