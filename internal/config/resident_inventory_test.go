package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

// connectorTools classifies one connector's tools (short names, without the
// mcp__claude_ai_<Connector>__ prefix).
type connectorTools struct {
	read  []string // read-only: allowed, never ask/deny
	write []string // write/send/delete/modify (ambiguous => write): ask or deny, never allow
	draft []string // draft create/update: allowed
}

// residentConnectorInventory was taken on 2026-11-09 from the claude.ai
// connector tool list. It MUST be refreshed whenever a connector adds, renames
// or removes a tool: TestResidentConnectorInventory fails on any default
// allow/ask/deny entry for these connectors that is not listed here, so a new
// write tool cannot silently land in the allow tier. Ambiguous tools are
// classified as write (fail safe), e.g. Granola give_feedback/generate_summary,
// Linear prepare_*_upload, Slack get_file_upload_url, Notion complete-mcp-setup.
var residentConnectorInventory = map[string]connectorTools{
	"Notion": {
		read: []string{
			"notion-calendar-list-calendar-resources",
			"notion-calendar-list-calendars",
			"notion-calendar-list-contacts",
			"notion-calendar-list-coworkers-events",
			"notion-calendar-list-events",
			"notion-connector-search",
			"notion-download-attachment",
			"notion-download-skill",
			"notion-fetch",
			"notion-get-async-task",
			"notion-get-comments",
			"notion-get-enabled-skills",
			"notion-get-file-download-urls",
			"notion-get-groups",
			"notion-get-self",
			"notion-get-session-status",
			"notion-get-setup-guide",
			"notion-get-sidebar-tabs",
			"notion-get-suggested-edit",
			"notion-get-teams",
			"notion-get-tool-access",
			"notion-get-users",
			"notion-get-version-changes",
			"notion-list-favorite-pages",
			"notion-list-page-backlinks",
			"notion-list-private-pages",
			"notion-list-recent-pages",
			"notion-list-session-events",
			"notion-list-shared-pages",
			"notion-memory-search",
			"notion-query-data-sources",
			"notion-query-meeting-notes",
			"notion-query-multiple-data-sources",
			"notion-query-sessions",
			"notion-read-session-event",
			"notion-search",
			"notion-search-agents",
			"notion-search-sessions",
			"notion-view-version",
			"notion-wait-session",
		},
		write: []string{
			"notion-calendar-cancel-events",
			"notion-calendar-create-events",
			"notion-calendar-update-events",
			"notion-complete-mcp-setup",
			"notion-convert-page-to-skill",
			"notion-convert-to-meetings-database",
			"notion-create-attachment",
			"notion-create-comment",
			"notion-create-database",
			"notion-create-file-upload",
			"notion-create-folder",
			"notion-create-meeting-note",
			"notion-create-pages",
			"notion-create-suggested-edit",
			"notion-create-view",
			"notion-delete-comment",
			"notion-delete-databases",
			"notion-delete-pages",
			"notion-duplicate-page",
			"notion-link-meeting-note-to-calendar-event",
			"notion-move-pages",
			"notion-resolve-suggested-edit",
			"notion-restore-pages",
			"notion-restore-version",
			"notion-send-message-to-session",
			"notion-set-comment-reaction",
			"notion-set-discussion-resolved",
			"notion-spawn-session",
			"notion-stop-session",
			"notion-update-comment",
			"notion-update-data-source",
			"notion-update-folder",
			"notion-update-page",
			"notion-update-page-favorites",
			"notion-update-sidebar-tabs",
			"notion-update-skill-settings",
			"notion-update-view",
			"notion-upload-skill",
		},
		draft: []string{},
	},
	"Gmail": {
		read: []string{
			"get_draft",
			"get_message",
			"get_thread",
			"list_drafts",
			"list_labels",
			"search_threads",
		},
		write: []string{
			"apply_sensitive_message_label",
			"apply_sensitive_thread_label",
			"create_label",
			"delete_draft",
			"delete_label",
			"forward",
			"label_message",
			"label_thread",
			"mark_message_spam",
			"mark_thread_spam",
			"reply",
			"send_message",
			"trash_message",
			"trash_thread",
			"unlabel_message",
			"unlabel_thread",
			"unmark_message_spam",
			"unmark_thread_spam",
			"untrash_message",
			"untrash_thread",
			"update_label",
			"update_message_labels",
		},
		draft: []string{
			"create_draft",
			"update_draft",
		},
	},
	"Superhuman_Mail": {
		read: []string{
			"get_attachment",
			"get_availability",
			"get_draft",
			"get_draft_send_status",
			"get_message",
			"get_read_status_feed",
			"get_shared_thread_link",
			"get_snippet",
			"get_thread",
			"list_accounts",
			"list_drafts",
			"list_labels",
			"list_reminders",
			"list_shared_thread_comments",
			"list_shared_thread_participants",
			"list_snippets",
			"list_splits",
			"list_threads",
			"preview_comment",
			"preview_share_thread",
			"query_email_and_calendar",
		},
		write: []string{
			"add_account",
			"create_or_update_event",
			"create_or_update_reminder",
			"create_or_update_snippet",
			"create_shared_thread_comment",
			"delete_shared_thread_comment",
			"discard_draft",
			"discard_reminder",
			"discard_snippet",
			"invite_teammate",
			"mark_spam",
			"react_shared_thread_comment",
			"remove_account",
			"remove_attachment",
			"send_draft",
			"set_default_account",
			"share_thread",
			"trash_thread",
			"undo_send",
			"unreact_shared_thread_comment",
			"unshare_thread",
			"unsubscribe",
			"update_personalization",
			"update_thread",
			"upload_attachment",
		},
		draft: []string{
			"create_or_update_draft",
		},
	},
	"Slack": {
		read: []string{
			"slack_get_reactions",
			"slack_list_channel_members",
			"slack_list_user_channels",
			"slack_read_canvas",
			"slack_read_channel",
			"slack_read_file",
			"slack_read_list",
			"slack_read_thread",
			"slack_read_user_profile",
			"slack_search_channels",
			"slack_search_emojis",
			"slack_search_public",
			"slack_search_public_and_private",
			"slack_search_users",
		},
		write: []string{
			"slack_add_list_record",
			"slack_add_reaction",
			"slack_complete_file_upload",
			"slack_create_canvas",
			"slack_create_conversation",
			"slack_create_list",
			"slack_get_file_upload_url",
			"slack_schedule_message",
			"slack_send_message",
			"slack_update_canvas",
			"slack_update_list",
			"slack_update_list_record",
		},
		draft: []string{
			"slack_send_message_draft",
		},
	},
	"Linear": {
		read: []string{
			"extract_images",
			"get_agent_skill",
			"get_attachment",
			"get_diff",
			"get_diff_threads",
			"get_document",
			"get_initiative",
			"get_issue",
			"get_issue_status",
			"get_milestone",
			"get_notifications",
			"get_project",
			"get_release",
			"get_release_note",
			"get_status_updates",
			"get_team",
			"get_template",
			"get_triage_responsibility",
			"get_user",
			"get_workspace",
			"list_agent_skills",
			"list_comments",
			"list_custom_views",
			"list_customer_needs",
			"list_customers",
			"list_cycles",
			"list_diffs",
			"list_documents",
			"list_initiative_labels",
			"list_initiatives",
			"list_issue_labels",
			"list_issue_statuses",
			"list_issues",
			"list_milestones",
			"list_project_labels",
			"list_projects",
			"list_release_notes",
			"list_release_pipelines",
			"list_releases",
			"list_teams",
			"list_templates",
			"list_users",
			"search_documentation",
			"whoami",
		},
		write: []string{
			"create_attachment",
			"create_attachment_from_upload",
			"create_initiative_label",
			"create_issue_label",
			"delete_attachment",
			"delete_comment",
			"delete_customer",
			"delete_customer_need",
			"delete_diff_comment",
			"delete_status_update",
			"mark_notification",
			"merge_diff",
			"prepare_attachment_upload",
			"prepare_file_upload",
			"resolve_diff_thread",
			"restore_initiative_label",
			"restore_issue_label",
			"restore_project_label",
			"retire_initiative_label",
			"retire_issue_label",
			"retire_project_label",
			"save_comment",
			"save_customer",
			"save_customer_need",
			"save_diff_comment",
			"save_document",
			"save_initiative",
			"save_initiative_label",
			"save_issue",
			"save_issue_label",
			"save_milestone",
			"save_project",
			"save_project_label",
			"save_release",
			"save_release_note",
			"save_status_update",
			"share_issue",
			"submit_diff_review",
			"unshare_issue",
			"update_diff",
		},
		draft: []string{},
	},
	"Google_Calendar": {
		read: []string{
			"get_event",
			"list_calendars",
			"list_events",
			"search_events",
			"suggest_time",
		},
		write: []string{
			"create_event",
			"delete_event",
			"respond_to_event",
			"update_event",
		},
		draft: []string{},
	},
	"Granola": {
		read: []string{
			"get_account_info",
			"get_meeting_attachment",
			"get_meeting_transcript",
			"get_meetings",
			"get_my_current_work_context",
			"list_meeting_folders",
			"list_templates",
			"query_granola_meetings",
			"search_meetings",
			"search_transcripts",
		},
		write: []string{
			"add_note_to_folder",
			"create_folder",
			"generate_summary",
			"give_feedback",
			"remove_note_from_folder",
			"sign_out",
			"update_folder",
			"update_private_notes",
		},
		draft: []string{},
	},
}

func TestResidentConnectorInventory(t *testing.T) {
	d := DefaultResidentConfig(1)
	var allow, ask, deny = d.AllowTools, d.AskTools, d.DenyTools

	known := map[string]string{} // full name -> class
	for c, ct := range residentConnectorInventory {
		pre := "mcp__claude_ai_" + c + "__"
		for class, names := range map[string][]string{"read": ct.read, "write": ct.write, "draft": ct.draft} {
			for _, n := range names {
				full := pre + n
				if prev, dup := known[full]; dup {
					t.Errorf("%s classified twice (%s, %s)", full, prev, class)
				}
				known[full] = class
			}
		}
	}

	t.Run("every write is ask or deny, never allow", func(t *testing.T) {
		for full, class := range known {
			if class != "write" {
				continue
			}
			testutil.Equal(t, slices.Contains(allow, full), false)
			if !slices.Contains(ask, full) && !slices.Contains(deny, full) {
				t.Errorf("write tool %s is in neither ask nor deny", full)
			}
		}
	})
	t.Run("every draft tool is allowed", func(t *testing.T) {
		for full, class := range known {
			if class == "draft" && !slices.Contains(allow, full) {
				t.Errorf("draft tool %s is not allowed", full)
			}
			if class == "draft" && (slices.Contains(ask, full) || slices.Contains(deny, full)) {
				t.Errorf("draft tool %s must not be ask/deny", full)
			}
		}
	})
	t.Run("reads are not ask or deny", func(t *testing.T) {
		for full, class := range known {
			if class == "read" && (slices.Contains(ask, full) || slices.Contains(deny, full)) {
				t.Errorf("read tool %s is ask/deny", full)
			}
		}
	})
	t.Run("no default entry is missing from the inventory", func(t *testing.T) {
		for tier, list := range map[string][]string{"allow": allow, "ask": ask, "deny": deny} {
			for _, full := range list {
				if !strings.HasPrefix(full, "mcp__claude_ai_") {
					continue
				}
				c := strings.SplitN(strings.TrimPrefix(full, "mcp__claude_ai_"), "__", 2)[0]
				if _, tracked := residentConnectorInventory[c]; !tracked {
					continue
				}
				if _, ok := known[full]; !ok {
					t.Errorf("%s entry %s is not in residentConnectorInventory; classify it", tier, full)
				}
			}
		}
	})
	t.Run("slack send and schedule are denied", func(t *testing.T) {
		for _, n := range []string{"slack_send_message", "slack_schedule_message"} {
			testutil.Equal(t, slices.Contains(deny, "mcp__claude_ai_Slack__"+n), true)
		}
	})
}
