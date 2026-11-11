package config

// AgentsConfig controls shared agent launch preparation.
type AgentsConfig struct {
	TrustWorktrees bool `toml:"trust_worktrees"`
}

func defaultAgentsConfig() AgentsConfig {
	return AgentsConfig{TrustWorktrees: true}
}
