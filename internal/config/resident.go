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
	// AllowTools run without a prompt (the read-only tier: kb_*, connector
	// reads, task_recycle). The resident runs in a non-bypass permission mode
	// so ask/deny are enforced, which means anything unlisted would prompt.
	AllowTools []string `toml:"allow_tools"`
	// AskTools require a confirmation; DenyTools are refused outright.
	AskTools  []string `toml:"ask_tools"`
	DenyTools []string `toml:"deny_tools"`
}

// DefaultResidentConfig returns the shipped [resident] defaults.
func DefaultResidentConfig(budget int) ResidentConfig {
	return ResidentConfig{
		ContextBudget: budget,
		MemoryDir:     "resident",
		AllowTools: []string{
			"mcp__argus__kb_search", "mcp__argus__kb_read", "mcp__argus__kb_list", "mcp__argus__kb_ingest",
			"mcp__claude_ai_Google_Calendar__list_calendars", "mcp__claude_ai_Google_Calendar__list_events",
			"mcp__claude_ai_Google_Calendar__get_event", "mcp__claude_ai_Google_Calendar__search_events",
			"mcp__claude_ai_Google_Calendar__suggest_time",
			"mcp__claude_ai_Granola__get_meetings", "mcp__claude_ai_Granola__search_meetings",
			"mcp__claude_ai_Granola__search_transcripts", "mcp__claude_ai_Granola__get_meeting_transcript",
			"mcp__claude_ai_Granola__query_granola_meetings", "mcp__claude_ai_Granola__get_my_current_work_context",
			"mcp__claude_ai_Granola__list_meeting_folders",
			"mcp__claude_ai_Gmail__search_threads", "mcp__claude_ai_Gmail__get_thread", "mcp__claude_ai_Gmail__get_message",
			"mcp__claude_ai_Gmail__list_labels", "mcp__claude_ai_Gmail__list_drafts", "mcp__claude_ai_Gmail__get_draft",
			"mcp__claude_ai_Superhuman_Mail__list_threads", "mcp__claude_ai_Superhuman_Mail__get_thread",
			"mcp__claude_ai_Superhuman_Mail__get_message", "mcp__claude_ai_Superhuman_Mail__list_accounts",
			"mcp__claude_ai_Superhuman_Mail__query_email_and_calendar", "mcp__claude_ai_Superhuman_Mail__get_availability",
			"mcp__claude_ai_Slack__slack_read_channel", "mcp__claude_ai_Slack__slack_read_thread",
			"mcp__claude_ai_Slack__slack_read_user_profile", "mcp__claude_ai_Slack__slack_search_channels",
			"mcp__claude_ai_Slack__slack_search_public", "mcp__claude_ai_Slack__slack_search_public_and_private",
			"mcp__claude_ai_Slack__slack_search_users", "mcp__claude_ai_Slack__slack_read_canvas",
			"mcp__claude_ai_Slack__slack_send_message_draft",
			"mcp__claude_ai_Notion__notion-fetch", "mcp__claude_ai_Notion__notion-search",
			"mcp__claude_ai_Notion__notion-get-users", "mcp__claude_ai_Notion__notion-get-comments",
			"mcp__claude_ai_Notion__notion-query-meeting-notes",
			"mcp__claude_ai_Linear__get_issue", "mcp__claude_ai_Linear__list_issues", "mcp__claude_ai_Linear__list_comments",
			"mcp__claude_ai_Linear__get_project", "mcp__claude_ai_Linear__list_projects", "mcp__claude_ai_Linear__list_users",
			"mcp__claude_ai_Linear__get_user", "mcp__claude_ai_Linear__list_teams",
		},
		AskTools: []string{
			// task_recycle resolves its target from a caller-supplied id/cwd, so an
			// injected resident could recycle ANOTHER task; one confirmation per
			// recycle is the cheap guard until the tool binds to the caller.
			"mcp__argus__task_recycle",
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
