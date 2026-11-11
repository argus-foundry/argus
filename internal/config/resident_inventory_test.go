package config

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

// Read the actual schemas: a newly registered Hera tool must be assigned a
// reviewed tier explicitly, rather than silently gaining resident authority.
func TestResidentHeraInventory(t *testing.T) {
	r := DefaultResidentConfig(100)
	tiers := map[string]string{}
	for _, group := range []struct {
		tier  string
		names []string
	}{{"allow", r.AllowTools}, {"ask", r.AskTools}, {"deny", r.DenyTools}} {
		for _, name := range group.names {
			if strings.HasPrefix(name, "mcp__argus__hera_") {
				testutil.Equal(t, tiers[name], "")
				tiers[name] = group.tier
			}
		}
	}
	want := map[string]string{}
	for _, name := range []string{"send", "inbox", "get_messages", "mark_read", "status", "tree_updates", "accept", "revive", "unblock", "board_list", "board_post", "board_update", "board_resolve", "spawn_worker", "delete_worker"} {
		want["mcp__argus__hera_"+name] = "allow"
	}
	for _, name := range []string{"new_orchestrator", "join", "move", "rebind", "plan", "plan_node", "plan_node_update", "plan_node_cancel", "block"} {
		want["mcp__argus__hera_"+name] = "deny"
	}
	want["mcp__argus__hera_graduate_worker"] = "ask"
	testutil.DeepEqual(t, tiers, want)
	files, err := filepath.Glob("../mcp/*.go")
	testutil.NoError(t, err)
	re := regexp.MustCompile(`Name:\s*"(hera_[a-z_]*[a-z])"`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		testutil.NoError(t, err)
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			name := "mcp__argus__" + m[1]
			t.Run(m[1], func(t *testing.T) { testutil.True(t, want[name] != "") })
		}
	}
}

// connectorTools classifies one connector's tools (short names, without the
// mcp__claude_ai_<Connector>__ prefix).
type connectorTools struct {
	read  []string // read-only: allowed, never ask/deny
	write []string // write/send/delete/modify (ambiguous => write): ask or deny, never allow
	draft []string // draft create/update: allowed
}

// residentConnectorInventory was refreshed on 2026-11-10 from the live resident deferred-tool catalog and claude.ai
// connector tool list. It MUST be refreshed whenever a connector adds, renames
// or removes a tool: TestResidentConnectorInventory fails on any default
// allow/ask/deny entry for these connectors that is not listed here, so a new
// write tool cannot silently land in the allow tier. Ambiguous tools are
// classified as write (fail safe), e.g. Granola give_feedback/generate_summary,
// Linear prepare_*_upload, Slack get_file_upload_url, Notion complete-mcp-setup.
var residentConnectorInventory = map[string]connectorTools{
	"Keystone": {
		read: []string{
			"admin_actions",
			"ai_usage_metrics",
			"ai_usage_user_metrics",
			"app_preview_analyze_brand_quality",
			"app_preview_compare_merchants",
			"app_preview_get_brand_assets",
			"app_preview_get_merchant_fonts",
			"app_preview_list_merchants",
			"app_preview_preview_merchant",
			"approved_mcp_servers",
			"catalyst_account_attributes",
			"catalyst_accounts",
			"catalyst_health_scores",
			"catalyst_playbooks",
			"circleci_job_artifacts",
			"circleci_job_logs",
			"circleci_pipeline_status",
			"circleci_pipelines",
			"circleci_test_results",
			"circleci_workflow_status",
			"cloudflare_cache_analytics",
			"cloudflare_dns_records",
			"cloudflare_firewall_rules",
			"cloudflare_http_analytics",
			"cloudflare_page_rules",
			"cloudflare_security_analytics",
			"cloudflare_ssl_settings",
			"cloudflare_zone",
			"cloudflare_zones",
			"cloudwatch_alarms",
			"cloudwatch_list_metrics",
			"cloudwatch_logs_describe_log_groups",
			"cloudwatch_logs_describe_log_streams",
			"cloudwatch_logs_filter_log_events",
			"cloudwatch_metrics",
			"crossbeam_account_overlaps",
			"crossbeam_account_overlaps_search",
			"crossbeam_account_records",
			"crossbeam_account_records_search",
			"crossbeam_account_signals",
			"crossbeam_audit_logs",
			"crossbeam_current_user",
			"crossbeam_lead_overlaps",
			"crossbeam_lead_overlaps_search",
			"crossbeam_lead_records",
			"crossbeam_lead_records_search",
			"crossbeam_lead_signals",
			"crossbeam_organization_users",
			"crossbeam_own_deal_signals",
			"crossbeam_partner",
			"crossbeam_partner_population",
			"crossbeam_partner_populations",
			"crossbeam_partner_tags",
			"crossbeam_partners",
			"crossbeam_population",
			"crossbeam_populations",
			"crossbeam_reports",
			"datadog_logs",
			"datadog_metrics",
			"datadog_monitors",
			"datadog_rum_events",
			"datadog_rum_session",
			"datadog_services",
			"datadog_spans",
			"datadog_trace_to_rum",
			"deploy_context_for_ticket",
			"drata_policies",
			"drata_policy",
			"ecr_describe_images",
			"ecr_list_images",
			"ecs_clusters",
			"ecs_deployments",
			"ecs_list_tasks",
			"ecs_services",
			"ecs_task_definition",
			"ecs_task_health",
			"ecs_task_logs",
			"efs_access_points",
			"efs_file_systems",
			"efs_mount_targets",
			"elbv2_target_group_metrics",
			"elbv2_target_groups",
			"elbv2_target_health",
			"find_files",
			"front_download_attachment",
			"front_get_contact",
			"front_get_conversation",
			"front_list_contact_conversations",
			"front_list_contacts",
			"front_list_conversation_messages",
			"front_list_conversations",
			"front_list_inboxes",
			"front_list_tags",
			"front_list_teammates",
			"front_search_conversations",
			"get_api_documentation",
			"get_otel_configuration",
			"git_blame",
			"git_diff",
			"git_file_history",
			"git_show_commit",
			"github_linked_pr_status",
			"github_pr_diff",
			"github_prs_for_ticket",
			"github_recent_merges",
			"gong_call_transcripts",
			"gong_get_call",
			"gong_get_user",
			"gong_interaction_stats",
			"gong_list_calls",
			"gong_list_users",
			"jira_archive_cycle_time",
			"jira_archive_get_changelog",
			"jira_archive_get_comments",
			"jira_archive_get_issue",
			"jira_archive_search_issues",
			"jira_archive_similar",
			"knowledge_entity_types",
			"knowledge_extraction_audit",
			"knowledge_get_entity",
			"knowledge_get_facts",
			"knowledge_metrics",
			"knowledge_search",
			"knowledge_status",
			"langfuse_observation_detail",
			"langfuse_observations",
			"langfuse_trace_detail",
			"langfuse_traces",
			"linear_cycle_velocity",
			"linear_get_active_cycle",
			"linear_get_comments",
			"linear_get_cycle",
			"linear_get_document",
			"linear_get_initiative",
			"linear_get_initiative_updates",
			"linear_get_issue",
			"linear_get_issue_history",
			"linear_get_project",
			"linear_get_project_updates",
			"linear_get_user",
			"linear_list_cycles",
			"linear_list_initiatives",
			"linear_list_projects",
			"linear_list_teams",
			"linear_list_templates",
			"linear_list_users",
			"linear_list_workflow_states",
			"linear_search_documents",
			"linear_search_issues",
			"linear_similar",
			"list_directory",
			"list_replica_databases",
			"list_repositories",
			"list_snowflake_databases",
			"looker_get_dashboard",
			"looker_list_models",
			"looker_run_look",
			"looker_run_query",
			"looker_search_dashboards",
			"looker_search_looks",
			"merchant_config_snapshot",
			"mobile_app_rating_history",
			"mobile_app_releases",
			"mobile_app_review_summary",
			"mobile_app_reviews",
			"mobile_apps",
			"notion_get_comments",
			"notion_get_database",
			"notion_get_page",
			"notion_get_page_content",
			"notion_query_database",
			"notion_search",
			"production_blast_radius",
			"rds_instances",
			"rds_metrics",
			"read_file",
			"release_cut_status",
			"replica_query",
			"repo_ai_coding_metrics",
			"resolve_private_ip",
			"salesforce_describe",
			"salesforce_limits",
			"salesforce_query",
			"salesforce_search",
			"schema_column_info",
			"schema_table_info",
			"search_code",
			"secretsmanager_describe_secret",
			"secretsmanager_list_secrets",
			"sketch_diff_blueprint_versions",
			"sketch_get_blueprint",
			"sketch_get_blueprint_comments",
			"sketch_get_blueprint_node_comments",
			"sketch_get_blueprint_pending_changes",
			"sketch_get_blueprint_proposal",
			"sketch_get_blueprint_proposal_comments",
			"sketch_get_blueprint_proposal_mint_preview",
			"sketch_get_blueprint_requirement_comments",
			"sketch_get_blueprint_requirement_decision_story",
			"sketch_get_blueprint_thumbnail",
			"sketch_get_blueprint_version",
			"sketch_get_prototype",
			"sketch_get_prototype_file",
			"sketch_get_prototype_manifest",
			"sketch_get_prototype_source",
			"sketch_list_blueprint_proposals",
			"sketch_list_blueprint_versions",
			"sketch_list_blueprints",
			"sketch_search_blueprints",
			"snowflake_query",
			"snowflake_query_history",
			"snowflake_query_profile",
			"ssm_describe_parameters",
			"ssm_get_parameter",
			"vcr_cassette_classify",
			"windmill_failed_jobs",
			"windmill_flow",
			"windmill_flows",
			"windmill_job",
			"windmill_job_logs",
			"windmill_jobs",
			"windmill_script",
			"windmill_scripts",
			"yakul_get_policy",
			"yakul_get_run",
			"yakul_get_session_transcript",
			"yakul_get_skill",
			"yakul_list_activity",
			"yakul_list_repos",
			"yakul_list_runs",
			"yakul_list_sessions",
			"yakul_list_skills",
			"yakul_spend",
		},
		write: []string{
			"app_preview_refresh_merchants",
			"circleci_rerun_workflow",
			"assistant_ask",
		},
		draft: []string{},
	},
	"Figma": {
		read: []string{
			"download_assets",
			"get_code_connect_map",
			"get_code_connect_suggestions",
			"get_comments",
			"get_context_for_code_connect",
			"get_design_context",
			"get_figjam",
			"get_figma_skill",
			"get_generative_plugin",
			"get_libraries",
			"get_metadata",
			"get_motion_context",
			"get_screenshot",
			"get_shader",
			"get_variable_defs",
			"list_file_shaders",
			"list_generative_plugins",
			"list_shaders",
			"search_design_system",
			"weave_find_model",
			"weave_get_model_run_output",
			"weave_get_tool_inputs",
			"weave_get_tool_run_output",
			"weave_list_tools",
			"whoami",
		},
		write: []string{
			"add_code_connect_map",
			"create_generative_plugin",
			"create_new_file",
			"create_session",
			"create_shader",
			"export_video",
			"generate_diagram",
			"generate_image",
			"generate_vector_illustration",
			"send_code_connect_mappings",
			"send_code_to_figma",
			"update_generative_plugin",
			"update_shader",
			"upload_assets",
			"use_figma",
			"weave_cancel_tool_run",
			"weave_run_model",
			"weave_run_tool",
			"weave_upload_asset",
		},
		draft: []string{},
	},
	"Sketch": {
		read: []string{
			"blueprint_diff",
			"blueprint_get_proposal",
			"blueprint_list",
			"blueprint_list_proposals",
			"blueprint_mint_preview",
			"blueprint_pending_changes",
			"blueprint_requirement_history",
			"blueprint_requirement_references",
			"blueprint_show",
			"blueprint_versions",
			"changelog_get",
			"changelog_list",
			"component_library_list",
			"component_library_show",
			"get_selection_context",
			"prototype_adoptable_export",
			"prototype_page_states",
			"prototype_provenance",
			"report_merchant_list",
		},
		write: []string{
			"blueprint_apply_comments_to_proposal",
			"blueprint_apply_to_proposal",
			"blueprint_archive_proposal",
			"blueprint_comments",
			"blueprint_create_proposal",
			"blueprint_mint_proposal",
			"blueprint_pin_proposal_design_slot",
			"blueprint_request_proposal_review",
			"blueprint_set_proposal_severity",
			"blueprint_unpin_proposal_design_slot",
			"blueprint_update",
			"report_merchant_star",
			"report_merchant_unstar",
		},
		draft: []string{},
	},
	"Claude_Docs": {
		read: []string{
			"export",
			"guide",
			"query",
			"read",
		},
		write: []string{
			"batch",
			"create",
			"delete",
			"update",
		},
		draft: []string{},
	},
	"claude-artifacts": {
		read: []string{
			"db_get",
			"db_list",
			"db_profiles",
			"db_rules_get",
			"db_usage",
			"db_whoami",
			"describe_capabilities",
			"describe_type",
			"get_artifact",
			"guide",
			"list_artifacts",
			"list_assets",
			"list_files",
			"list_types",
			"read_artifact",
			"read_asset",
			"read_comments",
			"read_declaration",
			"read_file",
		},
		write: []string{
			"copy_asset",
			"copy_file",
			"create_from_type",
			"db_query",
			"db_str_replace",
			"db_write",
			"declare_capabilities",
			"delete_artifact",
			"delete_asset",
			"edit_version",
			"pin_artifact",
			"publish_files",
			"publish_version",
			"reply_comment",
			"reply_shared_comment",
			"resolve_comment",
			"unpin_artifact",
			"unwatch",
			"upload_asset",
			"watch",
		},
		draft: []string{},
	},
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
		if c == "claude-artifacts" {
			pre = "mcp__claude-artifacts__"
		}
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

	t.Run("every external allow is a reviewed read or draft", func(t *testing.T) {
		for _, full := range allow {
			if !strings.HasPrefix(full, "mcp__") || strings.HasPrefix(full, "mcp__argus__") {
				continue
			}
			t.Run(full, func(t *testing.T) { testutil.True(t, known[full] == "read" || known[full] == "draft") })
		}
	})
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
	t.Run("reads are allowed without ask or deny", func(t *testing.T) {
		for full, class := range known {
			if class == "read" {
				t.Run(full, func(t *testing.T) { testutil.True(t, slices.Contains(allow, full)) })
			}
			if class == "read" && (slices.Contains(ask, full) || slices.Contains(deny, full)) {
				t.Errorf("read tool %s is ask/deny", full)
			}
		}
	})
	t.Run("no default entry is missing from the inventory", func(t *testing.T) {
		for tier, list := range map[string][]string{"allow": allow, "ask": ask, "deny": deny} {
			for _, full := range list {
				if strings.HasPrefix(full, "mcp__claude-artifacts__") {
					testutil.True(t, known[full] != "")
					continue
				}
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
