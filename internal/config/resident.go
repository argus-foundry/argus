package config

import (
	"errors"
	"fmt"
	"os"
	"unicode"
	"unicode/utf8"
)

// ResidentNameMaxRunes bounds the resident's display name.
const ResidentNameMaxRunes = 12

// ResidentConfig is the [resident] block (add-resident-agent). The resident has
// no default name: choosing one is the act of opting in.
type ResidentConfig struct {
	// Enabled defaults to false. It only takes effect with a valid Name and the
	// KB prerequisite (see Config.ResidentPrereq).
	Enabled bool `toml:"enabled"`
	// Name is the display name (1 to 12 printable runes). No default.
	Name string `toml:"name"`
	// ContextBudget is the context_size at/above which the resident is nudged to
	// recycle. Defaults to the coordinator budget.
	ContextBudget int `toml:"context_budget"`
	// MemoryDir is the vault-relative working-memory folder. Independent of Name.
	MemoryDir string `toml:"memory_dir"`
	// Model and Account optionally override the resolved model / account.
	Model   string `toml:"model"`
	Account string `toml:"account"`
	// AskTools require a confirmation; DenyTools are refused outright.
	AskTools  []string `toml:"ask_tools"`
	DenyTools []string `toml:"deny_tools"`
}

// DefaultResidentConfig returns the shipped [resident] defaults.
func DefaultResidentConfig(budget int) ResidentConfig {
	return ResidentConfig{
		ContextBudget: budget,
		MemoryDir:     "resident",
		AskTools: []string{
			"mcp__claude_ai_Google_Calendar__create_event",
			"mcp__claude_ai_Google_Calendar__update_event",
			"mcp__claude_ai_Google_Calendar__delete_event",
			"mcp__claude_ai_Google_Calendar__respond_to_event",
			"mcp__claude_ai_Gmail__send_message",
			"mcp__claude_ai_Gmail__reply",
			"mcp__claude_ai_Gmail__forward",
			"mcp__claude_ai_Superhuman_Mail__send_draft",
			"mcp__claude_ai_Slack__slack_create_canvas",
			"mcp__claude_ai_Slack__slack_create_list",
		},
		DenyTools: []string{
			"mcp__claude_ai_Slack__slack_send_message",
			"mcp__claude_ai_Slack__slack_schedule_message",
		},
	}
}

// ValidResidentName reports whether s is a legal resident display name: 1 to
// ResidentNameMaxRunes printable runes, no control characters.
func ValidResidentName(s string) error {
	n := utf8.RuneCountInString(s)
	if n < 1 || n > ResidentNameMaxRunes {
		return fmt.Errorf("resident name must be 1 to %d characters", ResidentNameMaxRunes)
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return errors.New("resident name must contain only printable characters")
		}
	}
	return nil
}

// ResidentPermissionsEnforced gates resident start: AskTools/DenyTools are only
// declarations until the launch path maps them to the session's permission
// rules (add-resident-agent Stage 5). While false, ResidentPrereq refuses so the
// advertised Slack-send denial and outward-action confirmation are never
// silently skipped. Stage 5 flips this to true in the same change that
// enforces the lists.
var ResidentPermissionsEnforced = true

// ResidentPrereq reports why the resident cannot start, or nil when it can: it
// must be enabled, carry a valid name, and have the knowledge base available
// (kb.enabled and an existing metis_vault_path directory).
func (c Config) ResidentPrereq() error {
	r := c.Resident
	if !r.Enabled {
		return errors.New("resident is not enabled")
	}
	if err := ValidResidentName(r.Name); err != nil {
		return fmt.Errorf("a valid name is required to enable the resident: %w", err)
	}
	if !ResidentPermissionsEnforced {
		return errors.New("resident permission policy (ask_tools/deny_tools) is not enforced yet; refusing to start")
	}
	if !c.KB.Enabled {
		return errors.New("kb.enabled must be true: the resident keeps its memory in the knowledge base")
	}
	if c.KB.MetisVaultPath == "" {
		return errors.New("kb.metis_vault_path is not set: the resident keeps its memory in the vault")
	}
	if fi, err := os.Stat(c.KB.MetisVaultPath); err != nil || !fi.IsDir() {
		return fmt.Errorf("kb.metis_vault_path %q is not an existing directory", c.KB.MetisVaultPath)
	}
	return nil
}
