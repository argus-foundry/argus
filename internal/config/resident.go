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
	MaxWorkers int `toml:"max_workers"`
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
	// reads and draft creation/update, task_recycle). The resident runs in a non-bypass permission mode
	// so ask/deny are enforced, which means anything unlisted would prompt.
	AllowTools []string `toml:"allow_tools"`
	// AskTools require a confirmation: every write/send/delete/modify tool of the
	// outward connectors except draft creation/update. DenyTools are refused
	// outright (Slack send and schedule). resident_inventory_test.go classifies
	// every connector tool so a new write tool cannot land in AllowTools.
	AskTools  []string `toml:"ask_tools"`
	DenyTools []string `toml:"deny_tools"`
}

// DefaultResidentConfig returns the shipped [resident] defaults.
func DefaultResidentConfig(budget int) ResidentConfig {
	return ResidentConfig{
		MaxWorkers:    8,
		ContextBudget: budget,
		MemoryDir:     "resident",
		AllowTools: []string{
			"mcp__argus__hera_send", "mcp__argus__hera_inbox", "mcp__argus__hera_get_messages", "mcp__argus__hera_mark_read", "mcp__argus__hera_status", "mcp__argus__hera_tree_updates", "mcp__argus__hera_accept", "mcp__argus__hera_revive", "mcp__argus__hera_unblock", "mcp__argus__hera_board_list", "mcp__argus__hera_board_post", "mcp__argus__hera_board_update", "mcp__argus__hera_board_resolve", "mcp__argus__hera_spawn_worker", "mcp__argus__hera_delete_worker",
			"mcp__argus__kb_search", "mcp__argus__kb_read", "mcp__argus__kb_list", "mcp__argus__kb_ingest",
			// Safe unprompted: the resident's MCP connection is bound to its own task
			// at spawn and task_recycle then refuses any other target.
			"mcp__argus__task_recycle",
			// The orientation tells the resident to read and acknowledge its
			// own inbox; both only touch its own messages.
			"mcp__argus__task_inbox", "mcp__argus__task_message_ack",
			// Notion reads and drafts.
			"mcp__claude_ai_Notion__notion-calendar-list-calendar-resources",
			"mcp__claude_ai_Notion__notion-calendar-list-calendars",
			"mcp__claude_ai_Notion__notion-calendar-list-contacts",
			"mcp__claude_ai_Notion__notion-calendar-list-coworkers-events",
			"mcp__claude_ai_Notion__notion-calendar-list-events",
			"mcp__claude_ai_Notion__notion-connector-search",
			"mcp__claude_ai_Notion__notion-fetch",
			"mcp__claude_ai_Notion__notion-get-async-task",
			"mcp__claude_ai_Notion__notion-get-comments",
			"mcp__claude_ai_Notion__notion-get-enabled-skills",
			"mcp__claude_ai_Notion__notion-get-file-download-urls",
			"mcp__claude_ai_Notion__notion-get-groups",
			"mcp__claude_ai_Notion__notion-get-self",
			"mcp__claude_ai_Notion__notion-get-session-status",
			"mcp__claude_ai_Notion__notion-get-setup-guide",
			"mcp__claude_ai_Notion__notion-get-sidebar-tabs",
			"mcp__claude_ai_Notion__notion-get-suggested-edit",
			"mcp__claude_ai_Notion__notion-get-teams",
			"mcp__claude_ai_Notion__notion-get-tool-access",
			"mcp__claude_ai_Notion__notion-get-users",
			"mcp__claude_ai_Notion__notion-get-version-changes",
			"mcp__claude_ai_Notion__notion-list-favorite-pages",
			"mcp__claude_ai_Notion__notion-list-page-backlinks",
			"mcp__claude_ai_Notion__notion-list-private-pages",
			"mcp__claude_ai_Notion__notion-list-recent-pages",
			"mcp__claude_ai_Notion__notion-list-session-events",
			"mcp__claude_ai_Notion__notion-list-shared-pages",
			"mcp__claude_ai_Notion__notion-memory-search",
			"mcp__claude_ai_Notion__notion-query-data-sources",
			"mcp__claude_ai_Notion__notion-query-meeting-notes",
			"mcp__claude_ai_Notion__notion-query-multiple-data-sources",
			"mcp__claude_ai_Notion__notion-query-sessions",
			"mcp__claude_ai_Notion__notion-read-session-event",
			"mcp__claude_ai_Notion__notion-search",
			"mcp__claude_ai_Notion__notion-search-agents",
			"mcp__claude_ai_Notion__notion-search-sessions",
			"mcp__claude_ai_Notion__notion-view-version",
			// Gmail reads and drafts.
			"mcp__claude_ai_Gmail__get_draft",
			"mcp__claude_ai_Gmail__get_message",
			"mcp__claude_ai_Gmail__get_thread",
			"mcp__claude_ai_Gmail__list_drafts",
			"mcp__claude_ai_Gmail__list_labels",
			"mcp__claude_ai_Gmail__search_threads",
			"mcp__claude_ai_Gmail__create_draft",
			"mcp__claude_ai_Gmail__update_draft",
			// Superhuman_Mail reads and drafts.
			"mcp__claude_ai_Superhuman_Mail__get_attachment",
			"mcp__claude_ai_Superhuman_Mail__get_availability",
			"mcp__claude_ai_Superhuman_Mail__get_draft",
			"mcp__claude_ai_Superhuman_Mail__get_draft_send_status",
			"mcp__claude_ai_Superhuman_Mail__get_message",
			"mcp__claude_ai_Superhuman_Mail__get_read_status_feed",
			"mcp__claude_ai_Superhuman_Mail__get_shared_thread_link",
			"mcp__claude_ai_Superhuman_Mail__get_snippet",
			"mcp__claude_ai_Superhuman_Mail__get_thread",
			"mcp__claude_ai_Superhuman_Mail__list_accounts",
			"mcp__claude_ai_Superhuman_Mail__list_drafts",
			"mcp__claude_ai_Superhuman_Mail__list_labels",
			"mcp__claude_ai_Superhuman_Mail__list_reminders",
			"mcp__claude_ai_Superhuman_Mail__list_shared_thread_comments",
			"mcp__claude_ai_Superhuman_Mail__list_shared_thread_participants",
			"mcp__claude_ai_Superhuman_Mail__list_snippets",
			"mcp__claude_ai_Superhuman_Mail__list_splits",
			"mcp__claude_ai_Superhuman_Mail__list_threads",
			"mcp__claude_ai_Superhuman_Mail__query_email_and_calendar",
			"mcp__claude_ai_Superhuman_Mail__create_or_update_draft",
			// Slack reads and drafts.
			"mcp__claude_ai_Slack__slack_get_reactions",
			"mcp__claude_ai_Slack__slack_list_channel_members",
			"mcp__claude_ai_Slack__slack_list_user_channels",
			"mcp__claude_ai_Slack__slack_read_canvas",
			"mcp__claude_ai_Slack__slack_read_channel",
			"mcp__claude_ai_Slack__slack_read_file",
			"mcp__claude_ai_Slack__slack_read_list",
			"mcp__claude_ai_Slack__slack_read_thread",
			"mcp__claude_ai_Slack__slack_read_user_profile",
			"mcp__claude_ai_Slack__slack_search_channels",
			"mcp__claude_ai_Slack__slack_search_emojis",
			"mcp__claude_ai_Slack__slack_search_public",
			"mcp__claude_ai_Slack__slack_search_public_and_private",
			"mcp__claude_ai_Slack__slack_search_users",
			"mcp__claude_ai_Slack__slack_send_message_draft",
			// Linear reads and drafts.
			"mcp__claude_ai_Linear__get_agent_skill",
			"mcp__claude_ai_Linear__get_attachment",
			"mcp__claude_ai_Linear__get_diff",
			"mcp__claude_ai_Linear__get_diff_threads",
			"mcp__claude_ai_Linear__get_document",
			"mcp__claude_ai_Linear__get_initiative",
			"mcp__claude_ai_Linear__get_issue",
			"mcp__claude_ai_Linear__get_issue_status",
			"mcp__claude_ai_Linear__get_milestone",
			"mcp__claude_ai_Linear__get_notifications",
			"mcp__claude_ai_Linear__get_project",
			"mcp__claude_ai_Linear__get_release",
			"mcp__claude_ai_Linear__get_release_note",
			"mcp__claude_ai_Linear__get_status_updates",
			"mcp__claude_ai_Linear__get_team",
			"mcp__claude_ai_Linear__get_template",
			"mcp__claude_ai_Linear__get_triage_responsibility",
			"mcp__claude_ai_Linear__get_user",
			"mcp__claude_ai_Linear__get_workspace",
			"mcp__claude_ai_Linear__list_agent_skills",
			"mcp__claude_ai_Linear__list_comments",
			"mcp__claude_ai_Linear__list_custom_views",
			"mcp__claude_ai_Linear__list_customer_needs",
			"mcp__claude_ai_Linear__list_customers",
			"mcp__claude_ai_Linear__list_cycles",
			"mcp__claude_ai_Linear__list_diffs",
			"mcp__claude_ai_Linear__list_documents",
			"mcp__claude_ai_Linear__list_initiative_labels",
			"mcp__claude_ai_Linear__list_initiatives",
			"mcp__claude_ai_Linear__list_issue_labels",
			"mcp__claude_ai_Linear__list_issue_statuses",
			"mcp__claude_ai_Linear__list_issues",
			"mcp__claude_ai_Linear__list_milestones",
			"mcp__claude_ai_Linear__list_project_labels",
			"mcp__claude_ai_Linear__list_projects",
			"mcp__claude_ai_Linear__list_release_notes",
			"mcp__claude_ai_Linear__list_release_pipelines",
			"mcp__claude_ai_Linear__list_releases",
			"mcp__claude_ai_Linear__list_teams",
			"mcp__claude_ai_Linear__list_templates",
			"mcp__claude_ai_Linear__list_users",
			"mcp__claude_ai_Linear__search_documentation",
			"mcp__claude_ai_Linear__whoami",
			// Google_Calendar reads and drafts.
			"mcp__claude_ai_Google_Calendar__get_event",
			"mcp__claude_ai_Google_Calendar__list_calendars",
			"mcp__claude_ai_Google_Calendar__list_events",
			"mcp__claude_ai_Google_Calendar__search_events",
			"mcp__claude_ai_Google_Calendar__suggest_time",
			// Granola reads and drafts.
			"mcp__claude_ai_Granola__get_account_info",
			"mcp__claude_ai_Granola__get_meeting_attachment",
			"mcp__claude_ai_Granola__get_meeting_transcript",
			"mcp__claude_ai_Granola__get_meetings",
			"mcp__claude_ai_Granola__get_my_current_work_context",
			"mcp__claude_ai_Granola__list_meeting_folders",
			"mcp__claude_ai_Granola__list_templates",
			"mcp__claude_ai_Granola__query_granola_meetings",
			"mcp__claude_ai_Granola__search_meetings",
			"mcp__claude_ai_Granola__search_transcripts",
		},
		AskTools: []string{
			"mcp__argus__hera_graduate_worker",
			// An internal side effect, but it types text into another agent's
			// session, so it needs confirmation.
			"mcp__argus__task_message_send",
			// Notion writes.
			"mcp__claude_ai_Notion__notion-calendar-cancel-events",
			"mcp__claude_ai_Notion__notion-calendar-create-events",
			"mcp__claude_ai_Notion__notion-calendar-update-events",
			"mcp__claude_ai_Notion__notion-complete-mcp-setup",
			"mcp__claude_ai_Notion__notion-convert-page-to-skill",
			"mcp__claude_ai_Notion__notion-convert-to-meetings-database",
			"mcp__claude_ai_Notion__notion-create-attachment",
			"mcp__claude_ai_Notion__notion-create-comment",
			"mcp__claude_ai_Notion__notion-create-database",
			"mcp__claude_ai_Notion__notion-create-file-upload",
			"mcp__claude_ai_Notion__notion-create-folder",
			"mcp__claude_ai_Notion__notion-create-meeting-note",
			"mcp__claude_ai_Notion__notion-create-pages",
			"mcp__claude_ai_Notion__notion-create-suggested-edit",
			"mcp__claude_ai_Notion__notion-create-view",
			"mcp__claude_ai_Notion__notion-delete-comment",
			"mcp__claude_ai_Notion__notion-delete-databases",
			"mcp__claude_ai_Notion__notion-delete-pages",
			"mcp__claude_ai_Notion__notion-duplicate-page",
			"mcp__claude_ai_Notion__notion-link-meeting-note-to-calendar-event",
			"mcp__claude_ai_Notion__notion-move-pages",
			"mcp__claude_ai_Notion__notion-resolve-suggested-edit",
			"mcp__claude_ai_Notion__notion-restore-pages",
			"mcp__claude_ai_Notion__notion-restore-version",
			"mcp__claude_ai_Notion__notion-send-message-to-session",
			"mcp__claude_ai_Notion__notion-set-comment-reaction",
			"mcp__claude_ai_Notion__notion-set-discussion-resolved",
			"mcp__claude_ai_Notion__notion-spawn-session",
			"mcp__claude_ai_Notion__notion-stop-session",
			"mcp__claude_ai_Notion__notion-update-comment",
			"mcp__claude_ai_Notion__notion-update-data-source",
			"mcp__claude_ai_Notion__notion-update-folder",
			"mcp__claude_ai_Notion__notion-update-page",
			"mcp__claude_ai_Notion__notion-update-page-favorites",
			"mcp__claude_ai_Notion__notion-update-sidebar-tabs",
			"mcp__claude_ai_Notion__notion-update-skill-settings",
			"mcp__claude_ai_Notion__notion-update-view",
			"mcp__claude_ai_Notion__notion-upload-skill",
			// Gmail writes.
			"mcp__claude_ai_Gmail__apply_sensitive_message_label",
			"mcp__claude_ai_Gmail__apply_sensitive_thread_label",
			"mcp__claude_ai_Gmail__create_label",
			"mcp__claude_ai_Gmail__delete_draft",
			"mcp__claude_ai_Gmail__delete_label",
			"mcp__claude_ai_Gmail__forward",
			"mcp__claude_ai_Gmail__label_message",
			"mcp__claude_ai_Gmail__label_thread",
			"mcp__claude_ai_Gmail__mark_message_spam",
			"mcp__claude_ai_Gmail__mark_thread_spam",
			"mcp__claude_ai_Gmail__reply",
			"mcp__claude_ai_Gmail__send_message",
			"mcp__claude_ai_Gmail__trash_message",
			"mcp__claude_ai_Gmail__trash_thread",
			"mcp__claude_ai_Gmail__unlabel_message",
			"mcp__claude_ai_Gmail__unlabel_thread",
			"mcp__claude_ai_Gmail__unmark_message_spam",
			"mcp__claude_ai_Gmail__unmark_thread_spam",
			"mcp__claude_ai_Gmail__untrash_message",
			"mcp__claude_ai_Gmail__untrash_thread",
			"mcp__claude_ai_Gmail__update_label",
			"mcp__claude_ai_Gmail__update_message_labels",
			// Superhuman_Mail writes.
			"mcp__claude_ai_Superhuman_Mail__add_account",
			"mcp__claude_ai_Superhuman_Mail__create_or_update_event",
			"mcp__claude_ai_Superhuman_Mail__create_or_update_reminder",
			"mcp__claude_ai_Superhuman_Mail__create_or_update_snippet",
			"mcp__claude_ai_Superhuman_Mail__create_shared_thread_comment",
			"mcp__claude_ai_Superhuman_Mail__delete_shared_thread_comment",
			"mcp__claude_ai_Superhuman_Mail__discard_draft",
			"mcp__claude_ai_Superhuman_Mail__discard_reminder",
			"mcp__claude_ai_Superhuman_Mail__discard_snippet",
			"mcp__claude_ai_Superhuman_Mail__invite_teammate",
			"mcp__claude_ai_Superhuman_Mail__mark_spam",
			"mcp__claude_ai_Superhuman_Mail__react_shared_thread_comment",
			"mcp__claude_ai_Superhuman_Mail__remove_account",
			"mcp__claude_ai_Superhuman_Mail__remove_attachment",
			"mcp__claude_ai_Superhuman_Mail__send_draft",
			"mcp__claude_ai_Superhuman_Mail__set_default_account",
			"mcp__claude_ai_Superhuman_Mail__share_thread",
			"mcp__claude_ai_Superhuman_Mail__trash_thread",
			"mcp__claude_ai_Superhuman_Mail__undo_send",
			"mcp__claude_ai_Superhuman_Mail__unreact_shared_thread_comment",
			"mcp__claude_ai_Superhuman_Mail__unshare_thread",
			"mcp__claude_ai_Superhuman_Mail__unsubscribe",
			"mcp__claude_ai_Superhuman_Mail__update_personalization",
			"mcp__claude_ai_Superhuman_Mail__update_thread",
			"mcp__claude_ai_Superhuman_Mail__upload_attachment",
			// Slack writes.
			"mcp__claude_ai_Slack__slack_add_list_record",
			"mcp__claude_ai_Slack__slack_add_reaction",
			"mcp__claude_ai_Slack__slack_complete_file_upload",
			"mcp__claude_ai_Slack__slack_create_canvas",
			"mcp__claude_ai_Slack__slack_create_conversation",
			"mcp__claude_ai_Slack__slack_create_list",
			"mcp__claude_ai_Slack__slack_get_file_upload_url",
			"mcp__claude_ai_Slack__slack_update_canvas",
			"mcp__claude_ai_Slack__slack_update_list",
			"mcp__claude_ai_Slack__slack_update_list_record",
			// Linear writes.
			"mcp__claude_ai_Linear__create_attachment",
			"mcp__claude_ai_Linear__create_attachment_from_upload",
			"mcp__claude_ai_Linear__create_initiative_label",
			"mcp__claude_ai_Linear__create_issue_label",
			"mcp__claude_ai_Linear__delete_attachment",
			"mcp__claude_ai_Linear__delete_comment",
			"mcp__claude_ai_Linear__delete_customer",
			"mcp__claude_ai_Linear__delete_customer_need",
			"mcp__claude_ai_Linear__delete_diff_comment",
			"mcp__claude_ai_Linear__delete_status_update",
			"mcp__claude_ai_Linear__mark_notification",
			"mcp__claude_ai_Linear__merge_diff",
			"mcp__claude_ai_Linear__prepare_attachment_upload",
			"mcp__claude_ai_Linear__prepare_file_upload",
			"mcp__claude_ai_Linear__resolve_diff_thread",
			"mcp__claude_ai_Linear__restore_initiative_label",
			"mcp__claude_ai_Linear__restore_issue_label",
			"mcp__claude_ai_Linear__restore_project_label",
			"mcp__claude_ai_Linear__retire_initiative_label",
			"mcp__claude_ai_Linear__retire_issue_label",
			"mcp__claude_ai_Linear__retire_project_label",
			"mcp__claude_ai_Linear__save_comment",
			"mcp__claude_ai_Linear__save_customer",
			"mcp__claude_ai_Linear__save_customer_need",
			"mcp__claude_ai_Linear__save_diff_comment",
			"mcp__claude_ai_Linear__save_document",
			"mcp__claude_ai_Linear__save_initiative",
			"mcp__claude_ai_Linear__save_initiative_label",
			"mcp__claude_ai_Linear__save_issue",
			"mcp__claude_ai_Linear__save_issue_label",
			"mcp__claude_ai_Linear__save_milestone",
			"mcp__claude_ai_Linear__save_project",
			"mcp__claude_ai_Linear__save_project_label",
			"mcp__claude_ai_Linear__save_release",
			"mcp__claude_ai_Linear__save_release_note",
			"mcp__claude_ai_Linear__save_status_update",
			"mcp__claude_ai_Linear__share_issue",
			"mcp__claude_ai_Linear__submit_diff_review",
			"mcp__claude_ai_Linear__unshare_issue",
			"mcp__claude_ai_Linear__update_diff",
			// Google_Calendar writes.
			"mcp__claude_ai_Google_Calendar__create_event",
			"mcp__claude_ai_Google_Calendar__delete_event",
			"mcp__claude_ai_Google_Calendar__respond_to_event",
			"mcp__claude_ai_Google_Calendar__update_event",
			// Granola writes.
			"mcp__claude_ai_Granola__add_note_to_folder",
			"mcp__claude_ai_Granola__create_folder",
			"mcp__claude_ai_Granola__generate_summary",
			"mcp__claude_ai_Granola__give_feedback",
			"mcp__claude_ai_Granola__remove_note_from_folder",
			"mcp__claude_ai_Granola__sign_out",
			"mcp__claude_ai_Granola__update_folder",
			"mcp__claude_ai_Granola__update_private_notes",
		},
		DenyTools: []string{
			"mcp__argus__hera_new_orchestrator", "mcp__argus__hera_join", "mcp__argus__hera_move", "mcp__argus__hera_rebind", "mcp__argus__hera_plan", "mcp__argus__hera_plan_node", "mcp__argus__hera_plan_node_update", "mcp__argus__hera_plan_node_cancel", "mcp__argus__hera_block",
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
// (kb.enabled and an existing metis_vault_path directory) and the REST API
// (api.enabled), which carries the resident's attention signals.
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
	if !c.API.Enabled {
		return errors.New("api.enabled must be true: the resident's reply tracking, context budget and attention signals go through the REST API (enable it in Settings -> API or set api.enabled = true in config.toml)")
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
