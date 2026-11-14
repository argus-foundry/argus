package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ResidentNameMaxRunes bounds the resident's display name.
const ResidentNameMaxRunes = 12

// ResidentConfig is the [resident] block (add-resident-agent). The resident has
// no default name: choosing one is the act of opting in.
type ResidentConfig struct {
	WorkerAccounts map[string]bool `toml:"worker_accounts"`
	MaxWorkers     int             `toml:"max_workers"`
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
	// AutoSwitchAccount (default true) lets the daemon move the resident to
	// another Claude account when its current one is out of usage budget. A
	// resident move (manual or automatic) stores a runtime override that
	// outranks Account until Account is edited.
	AutoSwitchAccount bool `toml:"auto_switch_account"`
	// AllowTools run ordinary built-ins, Argus tools and reviewed connector
	// reads/drafts without a prompt. The non-bypass default mode still asks
	// for unmatched tools; user ask/deny rules and structural Hera tiers win.
	AllowTools []string `toml:"allow_tools"`
	// AskTools require a confirmation: every write/send/delete/modify tool of the
	// outward connectors except draft creation/update. DenyTools are refused
	// outright (Slack send and schedule). resident_inventory_test.go classifies
	// every connector tool so a new write tool cannot land in AllowTools.
	AskTools  []string `toml:"ask_tools"`
	DenyTools []string `toml:"deny_tools"`
}

// ExpandResidentConnectorRules preserves literal tool names while emitting both
// historical display-name spellings and lowercase connector server names. Only
// these reviewed server identities are aliases; unfamiliar servers stay exact.
// Keep this at policy delivery, not just in defaults: persisted config overrides
// and the account's ask/deny restrictions need the same protection.
func ExpandResidentConnectorRules(rules []string) []string {
	servers := []string{
		"claude_ai_Keystone", "claude_ai_Notion", "claude_ai_Figma",
		"claude_ai_Sketch", "claude_ai_Claude_Docs", "claude_ai_Slack",
		"claude_ai_Gmail", "claude_ai_Superhuman_Mail", "claude_ai_Google_Calendar",
		"claude_ai_Linear", "claude_ai_Granola", "claude-artifacts",
	}
	var expanded []string
	seen := make(map[string]bool)
	add := func(rule string) {
		if !seen[rule] {
			seen[rule] = true
			expanded = append(expanded, rule)
		}
	}
	for _, rule := range rules {
		add(rule)
		if !strings.HasPrefix(rule, "mcp__") {
			continue
		}
		server, tool, ok := strings.Cut(strings.TrimPrefix(rule, "mcp__"), "__")
		if !ok || tool == "" {
			continue
		}
		for _, known := range servers {
			if strings.EqualFold(server, known) {
				add("mcp__" + known + "__" + tool)
				add("mcp__" + strings.ToLower(known) + "__" + tool)
				break
			}
		}
	}
	return expanded
}

// DefaultResidentConfig returns the shipped [resident] defaults.
func DefaultResidentConfig(budget int) ResidentConfig {
	return ResidentConfig{
		MaxWorkers:        8,
		AutoSwitchAccount: true,
		ContextBudget:     budget,
		MemoryDir:     "resident",
		AllowTools: []string{
			// Explicit built-ins: global allow wildcards are not supported by Claude.
			"Bash",
			"Read",
			"Write",
			"Edit",
			"MultiEdit",
			"Glob",
			"Grep",
			"LS",
			"WebFetch",
			"WebSearch",
			"Skill",
			"ToolSearch",
			"Agent",
			"Task",
			"TodoRead",
			"TodoWrite",
			"NotebookRead",
			"NotebookEdit",
			"TaskCreate",
			"TaskGet",
			"TaskList",
			"TaskUpdate",
			"TaskOutput",
			"TaskStop",
			"EnterPlanMode",
			"ExitPlanMode",
			"EnterWorktree",
			"ExitWorktree",
			"ListMcpResourcesTool",
			"ReadMcpResourceTool",
			"ReadMcpResourceDirTool",
			"CronCreate",
			"CronDelete",
			"CronList",
			"Monitor",
			// Broader Argus allow is subordinate to task_message_send and Hera ask/deny.
			"mcp__argus__*",
			// Keystone reads (reviewed catalog, 2026-11-10).
			"mcp__claude_ai_Keystone__admin_actions",
			"mcp__claude_ai_Keystone__ai_usage_metrics",
			"mcp__claude_ai_Keystone__ai_usage_user_metrics",
			"mcp__claude_ai_Keystone__app_preview_analyze_brand_quality",
			"mcp__claude_ai_Keystone__app_preview_compare_merchants",
			"mcp__claude_ai_Keystone__app_preview_get_brand_assets",
			"mcp__claude_ai_Keystone__app_preview_get_merchant_fonts",
			"mcp__claude_ai_Keystone__app_preview_list_merchants",
			"mcp__claude_ai_Keystone__app_preview_preview_merchant",
			"mcp__claude_ai_Keystone__approved_mcp_servers",
			"mcp__claude_ai_Keystone__catalyst_account_attributes",
			"mcp__claude_ai_Keystone__catalyst_accounts",
			"mcp__claude_ai_Keystone__catalyst_health_scores",
			"mcp__claude_ai_Keystone__catalyst_playbooks",
			"mcp__claude_ai_Keystone__circleci_job_artifacts",
			"mcp__claude_ai_Keystone__circleci_job_logs",
			"mcp__claude_ai_Keystone__circleci_pipeline_status",
			"mcp__claude_ai_Keystone__circleci_pipelines",
			"mcp__claude_ai_Keystone__circleci_test_results",
			"mcp__claude_ai_Keystone__circleci_workflow_status",
			"mcp__claude_ai_Keystone__cloudflare_cache_analytics",
			"mcp__claude_ai_Keystone__cloudflare_dns_records",
			"mcp__claude_ai_Keystone__cloudflare_firewall_rules",
			"mcp__claude_ai_Keystone__cloudflare_http_analytics",
			"mcp__claude_ai_Keystone__cloudflare_page_rules",
			"mcp__claude_ai_Keystone__cloudflare_security_analytics",
			"mcp__claude_ai_Keystone__cloudflare_ssl_settings",
			"mcp__claude_ai_Keystone__cloudflare_zone",
			"mcp__claude_ai_Keystone__cloudflare_zones",
			"mcp__claude_ai_Keystone__cloudwatch_alarms",
			"mcp__claude_ai_Keystone__cloudwatch_list_metrics",
			"mcp__claude_ai_Keystone__cloudwatch_logs_describe_log_groups",
			"mcp__claude_ai_Keystone__cloudwatch_logs_describe_log_streams",
			"mcp__claude_ai_Keystone__cloudwatch_logs_filter_log_events",
			"mcp__claude_ai_Keystone__cloudwatch_metrics",
			"mcp__claude_ai_Keystone__crossbeam_account_overlaps",
			"mcp__claude_ai_Keystone__crossbeam_account_overlaps_search",
			"mcp__claude_ai_Keystone__crossbeam_account_records",
			"mcp__claude_ai_Keystone__crossbeam_account_records_search",
			"mcp__claude_ai_Keystone__crossbeam_account_signals",
			"mcp__claude_ai_Keystone__crossbeam_audit_logs",
			"mcp__claude_ai_Keystone__crossbeam_current_user",
			"mcp__claude_ai_Keystone__crossbeam_lead_overlaps",
			"mcp__claude_ai_Keystone__crossbeam_lead_overlaps_search",
			"mcp__claude_ai_Keystone__crossbeam_lead_records",
			"mcp__claude_ai_Keystone__crossbeam_lead_records_search",
			"mcp__claude_ai_Keystone__crossbeam_lead_signals",
			"mcp__claude_ai_Keystone__crossbeam_organization_users",
			"mcp__claude_ai_Keystone__crossbeam_own_deal_signals",
			"mcp__claude_ai_Keystone__crossbeam_partner",
			"mcp__claude_ai_Keystone__crossbeam_partner_population",
			"mcp__claude_ai_Keystone__crossbeam_partner_populations",
			"mcp__claude_ai_Keystone__crossbeam_partner_tags",
			"mcp__claude_ai_Keystone__crossbeam_partners",
			"mcp__claude_ai_Keystone__crossbeam_population",
			"mcp__claude_ai_Keystone__crossbeam_populations",
			"mcp__claude_ai_Keystone__crossbeam_reports",
			"mcp__claude_ai_Keystone__datadog_logs",
			"mcp__claude_ai_Keystone__datadog_metrics",
			"mcp__claude_ai_Keystone__datadog_monitors",
			"mcp__claude_ai_Keystone__datadog_rum_events",
			"mcp__claude_ai_Keystone__datadog_rum_session",
			"mcp__claude_ai_Keystone__datadog_services",
			"mcp__claude_ai_Keystone__datadog_spans",
			"mcp__claude_ai_Keystone__datadog_trace_to_rum",
			"mcp__claude_ai_Keystone__deploy_context_for_ticket",
			"mcp__claude_ai_Keystone__drata_policies",
			"mcp__claude_ai_Keystone__drata_policy",
			"mcp__claude_ai_Keystone__ecr_describe_images",
			"mcp__claude_ai_Keystone__ecr_list_images",
			"mcp__claude_ai_Keystone__ecs_clusters",
			"mcp__claude_ai_Keystone__ecs_deployments",
			"mcp__claude_ai_Keystone__ecs_list_tasks",
			"mcp__claude_ai_Keystone__ecs_services",
			"mcp__claude_ai_Keystone__ecs_task_definition",
			"mcp__claude_ai_Keystone__ecs_task_health",
			"mcp__claude_ai_Keystone__ecs_task_logs",
			"mcp__claude_ai_Keystone__efs_access_points",
			"mcp__claude_ai_Keystone__efs_file_systems",
			"mcp__claude_ai_Keystone__efs_mount_targets",
			"mcp__claude_ai_Keystone__elbv2_target_group_metrics",
			"mcp__claude_ai_Keystone__elbv2_target_groups",
			"mcp__claude_ai_Keystone__elbv2_target_health",
			"mcp__claude_ai_Keystone__find_files",
			"mcp__claude_ai_Keystone__front_download_attachment",
			"mcp__claude_ai_Keystone__front_get_contact",
			"mcp__claude_ai_Keystone__front_get_conversation",
			"mcp__claude_ai_Keystone__front_list_contact_conversations",
			"mcp__claude_ai_Keystone__front_list_contacts",
			"mcp__claude_ai_Keystone__front_list_conversation_messages",
			"mcp__claude_ai_Keystone__front_list_conversations",
			"mcp__claude_ai_Keystone__front_list_inboxes",
			"mcp__claude_ai_Keystone__front_list_tags",
			"mcp__claude_ai_Keystone__front_list_teammates",
			"mcp__claude_ai_Keystone__front_search_conversations",
			"mcp__claude_ai_Keystone__get_api_documentation",
			"mcp__claude_ai_Keystone__get_otel_configuration",
			"mcp__claude_ai_Keystone__git_blame",
			"mcp__claude_ai_Keystone__git_diff",
			"mcp__claude_ai_Keystone__git_file_history",
			"mcp__claude_ai_Keystone__git_show_commit",
			"mcp__claude_ai_Keystone__github_linked_pr_status",
			"mcp__claude_ai_Keystone__github_pr_diff",
			"mcp__claude_ai_Keystone__github_prs_for_ticket",
			"mcp__claude_ai_Keystone__github_recent_merges",
			"mcp__claude_ai_Keystone__gong_call_transcripts",
			"mcp__claude_ai_Keystone__gong_get_call",
			"mcp__claude_ai_Keystone__gong_get_user",
			"mcp__claude_ai_Keystone__gong_interaction_stats",
			"mcp__claude_ai_Keystone__gong_list_calls",
			"mcp__claude_ai_Keystone__gong_list_users",
			"mcp__claude_ai_Keystone__jira_archive_cycle_time",
			"mcp__claude_ai_Keystone__jira_archive_get_changelog",
			"mcp__claude_ai_Keystone__jira_archive_get_comments",
			"mcp__claude_ai_Keystone__jira_archive_get_issue",
			"mcp__claude_ai_Keystone__jira_archive_search_issues",
			"mcp__claude_ai_Keystone__jira_archive_similar",
			"mcp__claude_ai_Keystone__knowledge_entity_types",
			"mcp__claude_ai_Keystone__knowledge_extraction_audit",
			"mcp__claude_ai_Keystone__knowledge_get_entity",
			"mcp__claude_ai_Keystone__knowledge_get_facts",
			"mcp__claude_ai_Keystone__knowledge_metrics",
			"mcp__claude_ai_Keystone__knowledge_search",
			"mcp__claude_ai_Keystone__knowledge_status",
			"mcp__claude_ai_Keystone__langfuse_observation_detail",
			"mcp__claude_ai_Keystone__langfuse_observations",
			"mcp__claude_ai_Keystone__langfuse_trace_detail",
			"mcp__claude_ai_Keystone__langfuse_traces",
			"mcp__claude_ai_Keystone__linear_cycle_velocity",
			"mcp__claude_ai_Keystone__linear_get_active_cycle",
			"mcp__claude_ai_Keystone__linear_get_comments",
			"mcp__claude_ai_Keystone__linear_get_cycle",
			"mcp__claude_ai_Keystone__linear_get_document",
			"mcp__claude_ai_Keystone__linear_get_initiative",
			"mcp__claude_ai_Keystone__linear_get_initiative_updates",
			"mcp__claude_ai_Keystone__linear_get_issue",
			"mcp__claude_ai_Keystone__linear_get_issue_history",
			"mcp__claude_ai_Keystone__linear_get_project",
			"mcp__claude_ai_Keystone__linear_get_project_updates",
			"mcp__claude_ai_Keystone__linear_get_user",
			"mcp__claude_ai_Keystone__linear_list_cycles",
			"mcp__claude_ai_Keystone__linear_list_initiatives",
			"mcp__claude_ai_Keystone__linear_list_projects",
			"mcp__claude_ai_Keystone__linear_list_teams",
			"mcp__claude_ai_Keystone__linear_list_templates",
			"mcp__claude_ai_Keystone__linear_list_users",
			"mcp__claude_ai_Keystone__linear_list_workflow_states",
			"mcp__claude_ai_Keystone__linear_search_documents",
			"mcp__claude_ai_Keystone__linear_search_issues",
			"mcp__claude_ai_Keystone__linear_similar",
			"mcp__claude_ai_Keystone__list_directory",
			"mcp__claude_ai_Keystone__list_replica_databases",
			"mcp__claude_ai_Keystone__list_repositories",
			"mcp__claude_ai_Keystone__list_snowflake_databases",
			"mcp__claude_ai_Keystone__looker_get_dashboard",
			"mcp__claude_ai_Keystone__looker_list_models",
			"mcp__claude_ai_Keystone__looker_run_look",
			"mcp__claude_ai_Keystone__looker_run_query",
			"mcp__claude_ai_Keystone__looker_search_dashboards",
			"mcp__claude_ai_Keystone__looker_search_looks",
			"mcp__claude_ai_Keystone__merchant_config_snapshot",
			"mcp__claude_ai_Keystone__mobile_app_rating_history",
			"mcp__claude_ai_Keystone__mobile_app_releases",
			"mcp__claude_ai_Keystone__mobile_app_review_summary",
			"mcp__claude_ai_Keystone__mobile_app_reviews",
			"mcp__claude_ai_Keystone__mobile_apps",
			"mcp__claude_ai_Keystone__notion_get_comments",
			"mcp__claude_ai_Keystone__notion_get_database",
			"mcp__claude_ai_Keystone__notion_get_page",
			"mcp__claude_ai_Keystone__notion_get_page_content",
			"mcp__claude_ai_Keystone__notion_query_database",
			"mcp__claude_ai_Keystone__notion_search",
			"mcp__claude_ai_Keystone__production_blast_radius",
			"mcp__claude_ai_Keystone__rds_instances",
			"mcp__claude_ai_Keystone__rds_metrics",
			"mcp__claude_ai_Keystone__read_file",
			"mcp__claude_ai_Keystone__release_cut_status",
			"mcp__claude_ai_Keystone__replica_query",
			"mcp__claude_ai_Keystone__repo_ai_coding_metrics",
			"mcp__claude_ai_Keystone__resolve_private_ip",
			"mcp__claude_ai_Keystone__salesforce_describe",
			"mcp__claude_ai_Keystone__salesforce_limits",
			"mcp__claude_ai_Keystone__salesforce_query",
			"mcp__claude_ai_Keystone__salesforce_search",
			"mcp__claude_ai_Keystone__schema_column_info",
			"mcp__claude_ai_Keystone__schema_table_info",
			"mcp__claude_ai_Keystone__search_code",
			"mcp__claude_ai_Keystone__secretsmanager_describe_secret",
			"mcp__claude_ai_Keystone__secretsmanager_list_secrets",
			"mcp__claude_ai_Keystone__sketch_diff_blueprint_versions",
			"mcp__claude_ai_Keystone__sketch_get_blueprint",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_comments",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_node_comments",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_pending_changes",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_proposal",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_proposal_comments",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_proposal_mint_preview",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_requirement_comments",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_requirement_decision_story",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_thumbnail",
			"mcp__claude_ai_Keystone__sketch_get_blueprint_version",
			"mcp__claude_ai_Keystone__sketch_get_prototype",
			"mcp__claude_ai_Keystone__sketch_get_prototype_file",
			"mcp__claude_ai_Keystone__sketch_get_prototype_manifest",
			"mcp__claude_ai_Keystone__sketch_get_prototype_source",
			"mcp__claude_ai_Keystone__sketch_list_blueprint_proposals",
			"mcp__claude_ai_Keystone__sketch_list_blueprint_versions",
			"mcp__claude_ai_Keystone__sketch_list_blueprints",
			"mcp__claude_ai_Keystone__sketch_search_blueprints",
			"mcp__claude_ai_Keystone__snowflake_query",
			"mcp__claude_ai_Keystone__snowflake_query_history",
			"mcp__claude_ai_Keystone__snowflake_query_profile",
			"mcp__claude_ai_Keystone__ssm_describe_parameters",
			"mcp__claude_ai_Keystone__ssm_get_parameter",
			"mcp__claude_ai_Keystone__vcr_cassette_classify",
			"mcp__claude_ai_Keystone__windmill_failed_jobs",
			"mcp__claude_ai_Keystone__windmill_flow",
			"mcp__claude_ai_Keystone__windmill_flows",
			"mcp__claude_ai_Keystone__windmill_job",
			"mcp__claude_ai_Keystone__windmill_job_logs",
			"mcp__claude_ai_Keystone__windmill_jobs",
			"mcp__claude_ai_Keystone__windmill_script",
			"mcp__claude_ai_Keystone__windmill_scripts",
			"mcp__claude_ai_Keystone__yakul_get_policy",
			"mcp__claude_ai_Keystone__yakul_get_run",
			"mcp__claude_ai_Keystone__yakul_get_session_transcript",
			"mcp__claude_ai_Keystone__yakul_get_skill",
			"mcp__claude_ai_Keystone__yakul_list_activity",
			"mcp__claude_ai_Keystone__yakul_list_repos",
			"mcp__claude_ai_Keystone__yakul_list_runs",
			"mcp__claude_ai_Keystone__yakul_list_sessions",
			"mcp__claude_ai_Keystone__yakul_list_skills",
			"mcp__claude_ai_Keystone__yakul_spend",
			// Figma reads (reviewed catalog, 2026-11-10).
			"mcp__claude_ai_Figma__download_assets",
			"mcp__claude_ai_Figma__get_code_connect_map",
			"mcp__claude_ai_Figma__get_code_connect_suggestions",
			"mcp__claude_ai_Figma__get_comments",
			"mcp__claude_ai_Figma__get_context_for_code_connect",
			"mcp__claude_ai_Figma__get_design_context",
			"mcp__claude_ai_Figma__get_figjam",
			"mcp__claude_ai_Figma__get_figma_skill",
			"mcp__claude_ai_Figma__get_generative_plugin",
			"mcp__claude_ai_Figma__get_libraries",
			"mcp__claude_ai_Figma__get_metadata",
			"mcp__claude_ai_Figma__get_motion_context",
			"mcp__claude_ai_Figma__get_screenshot",
			"mcp__claude_ai_Figma__get_shader",
			"mcp__claude_ai_Figma__get_variable_defs",
			"mcp__claude_ai_Figma__list_file_shaders",
			"mcp__claude_ai_Figma__list_generative_plugins",
			"mcp__claude_ai_Figma__list_shaders",
			"mcp__claude_ai_Figma__search_design_system",
			"mcp__claude_ai_Figma__weave_find_model",
			"mcp__claude_ai_Figma__weave_get_model_run_output",
			"mcp__claude_ai_Figma__weave_get_tool_inputs",
			"mcp__claude_ai_Figma__weave_get_tool_run_output",
			"mcp__claude_ai_Figma__weave_list_tools",
			"mcp__claude_ai_Figma__whoami",
			// Sketch reads (reviewed catalog, 2026-11-10).
			"mcp__claude_ai_Sketch__blueprint_diff",
			"mcp__claude_ai_Sketch__blueprint_get_proposal",
			"mcp__claude_ai_Sketch__blueprint_list",
			"mcp__claude_ai_Sketch__blueprint_list_proposals",
			"mcp__claude_ai_Sketch__blueprint_mint_preview",
			"mcp__claude_ai_Sketch__blueprint_pending_changes",
			"mcp__claude_ai_Sketch__blueprint_requirement_history",
			"mcp__claude_ai_Sketch__blueprint_requirement_references",
			"mcp__claude_ai_Sketch__blueprint_show",
			"mcp__claude_ai_Sketch__blueprint_versions",
			"mcp__claude_ai_Sketch__changelog_get",
			"mcp__claude_ai_Sketch__changelog_list",
			"mcp__claude_ai_Sketch__component_library_list",
			"mcp__claude_ai_Sketch__component_library_show",
			"mcp__claude_ai_Sketch__get_selection_context",
			"mcp__claude_ai_Sketch__prototype_adoptable_export",
			"mcp__claude_ai_Sketch__prototype_page_states",
			"mcp__claude_ai_Sketch__prototype_provenance",
			"mcp__claude_ai_Sketch__report_merchant_list",
			// Claude_Docs reads (reviewed catalog, 2026-11-10).
			"mcp__claude_ai_Claude_Docs__export",
			"mcp__claude_ai_Claude_Docs__guide",
			"mcp__claude_ai_Claude_Docs__query",
			"mcp__claude_ai_Claude_Docs__read",
			// claude-artifacts reads.
			"mcp__claude-artifacts__db_get",
			"mcp__claude-artifacts__db_list",
			"mcp__claude-artifacts__db_profiles",
			"mcp__claude-artifacts__db_rules_get",
			"mcp__claude-artifacts__db_usage",
			"mcp__claude-artifacts__db_whoami",
			"mcp__claude-artifacts__describe_capabilities",
			"mcp__claude-artifacts__describe_type",
			"mcp__claude-artifacts__get_artifact",
			"mcp__claude-artifacts__guide",
			"mcp__claude-artifacts__list_artifacts",
			"mcp__claude-artifacts__list_assets",
			"mcp__claude-artifacts__list_files",
			"mcp__claude-artifacts__list_types",
			"mcp__claude-artifacts__read_artifact",
			"mcp__claude-artifacts__read_asset",
			"mcp__claude-artifacts__read_comments",
			"mcp__claude-artifacts__read_declaration",
			"mcp__claude-artifacts__read_file",
			"mcp__argus__hera_send", "mcp__argus__hera_inbox", "mcp__argus__hera_get_messages", "mcp__argus__hera_mark_read", "mcp__argus__hera_status", "mcp__argus__hera_tree_updates", "mcp__argus__hera_accept", "mcp__argus__hera_revive", "mcp__argus__hera_unblock", "mcp__argus__hera_board_list", "mcp__argus__hera_board_post", "mcp__argus__hera_board_update", "mcp__argus__hera_board_resolve", "mcp__argus__hera_spawn_worker", "mcp__argus__hera_delete_worker",
			"mcp__argus__kb_search", "mcp__argus__kb_read", "mcp__argus__kb_list", "mcp__argus__kb_ingest",
			// Safe unprompted: the resident's MCP connection is bound to its own task
			// at spawn and task_recycle then refuses any other target.
			"mcp__argus__task_recycle",
			// The orientation tells the resident to read and acknowledge its
			// own inbox; both only touch its own messages.
			"mcp__argus__task_inbox", "mcp__argus__task_message_ack",
			// Notion reads and drafts.
			"mcp__claude_ai_Notion__notion-download-attachment",
			"mcp__claude_ai_Notion__notion-download-skill",
			"mcp__claude_ai_Notion__notion-wait-session",
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
			"mcp__claude_ai_Superhuman_Mail__preview_share_thread",
			"mcp__claude_ai_Superhuman_Mail__preview_comment",
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
			"mcp__claude_ai_Linear__extract_images",
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
			// Keystone writes and ambiguous operations.
			"mcp__claude_ai_Keystone__app_preview_refresh_merchants",
			"mcp__claude_ai_Keystone__circleci_rerun_workflow",
			"mcp__claude_ai_Keystone__assistant_ask",
			// Figma writes and ambiguous operations.
			"mcp__claude_ai_Figma__add_code_connect_map",
			"mcp__claude_ai_Figma__create_generative_plugin",
			"mcp__claude_ai_Figma__create_new_file",
			"mcp__claude_ai_Figma__create_session",
			"mcp__claude_ai_Figma__create_shader",
			"mcp__claude_ai_Figma__export_video",
			"mcp__claude_ai_Figma__generate_diagram",
			"mcp__claude_ai_Figma__generate_image",
			"mcp__claude_ai_Figma__generate_vector_illustration",
			"mcp__claude_ai_Figma__send_code_connect_mappings",
			"mcp__claude_ai_Figma__send_code_to_figma",
			"mcp__claude_ai_Figma__update_generative_plugin",
			"mcp__claude_ai_Figma__update_shader",
			"mcp__claude_ai_Figma__upload_assets",
			"mcp__claude_ai_Figma__use_figma",
			"mcp__claude_ai_Figma__weave_cancel_tool_run",
			"mcp__claude_ai_Figma__weave_run_model",
			"mcp__claude_ai_Figma__weave_run_tool",
			"mcp__claude_ai_Figma__weave_upload_asset",
			// Sketch writes and ambiguous operations.
			"mcp__claude_ai_Sketch__blueprint_apply_comments_to_proposal",
			"mcp__claude_ai_Sketch__blueprint_apply_to_proposal",
			"mcp__claude_ai_Sketch__blueprint_archive_proposal",
			"mcp__claude_ai_Sketch__blueprint_comments",
			"mcp__claude_ai_Sketch__blueprint_create_proposal",
			"mcp__claude_ai_Sketch__blueprint_mint_proposal",
			"mcp__claude_ai_Sketch__blueprint_pin_proposal_design_slot",
			"mcp__claude_ai_Sketch__blueprint_request_proposal_review",
			"mcp__claude_ai_Sketch__blueprint_set_proposal_severity",
			"mcp__claude_ai_Sketch__blueprint_unpin_proposal_design_slot",
			"mcp__claude_ai_Sketch__blueprint_update",
			"mcp__claude_ai_Sketch__report_merchant_star",
			"mcp__claude_ai_Sketch__report_merchant_unstar",
			// Claude_Docs writes and ambiguous operations.
			"mcp__claude_ai_Claude_Docs__batch",
			"mcp__claude_ai_Claude_Docs__create",
			"mcp__claude_ai_Claude_Docs__delete",
			"mcp__claude_ai_Claude_Docs__update",
			// claude-artifacts writes and ambiguous operations.
			"mcp__claude-artifacts__copy_asset",
			"mcp__claude-artifacts__copy_file",
			"mcp__claude-artifacts__create_from_type",
			"mcp__claude-artifacts__db_query",
			"mcp__claude-artifacts__db_str_replace",
			"mcp__claude-artifacts__db_write",
			"mcp__claude-artifacts__declare_capabilities",
			"mcp__claude-artifacts__delete_artifact",
			"mcp__claude-artifacts__delete_asset",
			"mcp__claude-artifacts__edit_version",
			"mcp__claude-artifacts__pin_artifact",
			"mcp__claude-artifacts__publish_files",
			"mcp__claude-artifacts__publish_version",
			"mcp__claude-artifacts__reply_comment",
			"mcp__claude-artifacts__reply_shared_comment",
			"mcp__claude-artifacts__resolve_comment",
			"mcp__claude-artifacts__unpin_artifact",
			"mcp__claude-artifacts__unwatch",
			"mcp__claude-artifacts__upload_asset",
			"mcp__claude-artifacts__watch",
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
