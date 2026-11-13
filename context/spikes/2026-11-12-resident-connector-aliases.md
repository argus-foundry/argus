# Resident connector server aliases

The resident's reviewed reads must remain allowed when a connector changes its server's capitalization. The 2026-11-12 resident transcript catalog names Keystone `claude_ai_keystone`, while the reviewed defaults use `claude_ai_Keystone` (236 rules). Tool names and classifications have not changed.

## Decision

Expand literal rules at policy delivery into the historical spelling and lowercase spelling of each reviewed server. Identify these server families case-insensitively, preserve the original rule, preserve the tool suffix byte-for-byte, and deduplicate. Apply the same expansion to resident allow/ask/deny, account user ask/deny, and the outward ask/deny tiers from which unattended-worker denials are derived. Loaded config overrides therefore receive the fix too; changing only shipped defaults would miss saved overrides.

This is a bounded alias list, not arbitrary server discovery or a wildcard grant. Unknown tools on these servers still ask in the resident. An entirely new server spelling or display name still needs review and an alias update. Arbitrary future capitalization is not promised: historical and lowercase spellings cover the observed transition without emitting exponentially many variants. No broad server or tool-name allow patterns are introduced. Workers retain their existing explicit-deny policy, including its documented unknown-tool limitation.

Launch-time catalog normalization was rejected: the connector tools arrive after Claude starts, and Argus has no catalog at BuildCmd time. Runtime interception would be a larger change than restoring these concrete grants.

## Catalog checked

Source: local resident session `d78d66c7-2b44-4828-a3fa-ccd706c6e068.jsonl`, inspected 2026-11-12. Only server identifiers were extracted; no customer content is copied here.

| Reviewed connector | Current server identifier | Additional emitted spelling |
| --- | --- | --- |
| Keystone | `claude_ai_keystone` | `claude_ai_Keystone` |
| Notion | `claude_ai_Notion` | `claude_ai_notion` |
| Figma | `claude_ai_Figma` | `claude_ai_figma` |
| Sketch | `claude_ai_Sketch` | `claude_ai_sketch` |
| Claude Docs | `claude_ai_Claude_Docs` | `claude_ai_claude_docs` |
| Slack | `claude_ai_Slack` | `claude_ai_slack` |
| Gmail | `claude_ai_Gmail` | `claude_ai_gmail` |
| Superhuman | `claude_ai_Superhuman_Mail` | `claude_ai_superhuman_mail` |
| Google Calendar | `claude_ai_Google_Calendar` | `claude_ai_google_calendar` |
| Linear | `claude_ai_Linear` | `claude_ai_linear` |
| Granola | `claude_ai_Granola` | `claude_ai_granola` |
| Artifacts | No `mcp__claude-artifacts__` tool identifier found in this transcript | Reviewed `claude-artifacts` identifier is already lowercase; unchanged |

The other reviewed claude.ai connectors have the same risk, even though their catalog names have not changed. Inventory tests assert all reviewed read/draft/write classifications under both spellings, and launch tests check Gong reads, CircleCI worker denials, Slack denials, unknown-tool fallback and user restrictions.

## Claude Code documentation check

Checked the official [permissions documentation](https://code.claude.com/docs/en/permissions#mcp) on 2026-11-12. Its MCP section instructs callers to use the server name as it appears in the tool name and gives `mcp__<server>__<tool>` for individual tools. The tool-name wildcard section requires a literal server prefix for allow rules; deny > ask > allow precedence is explicit.

The page does **not explicitly state that MCP permission matching is case-sensitive** or promise case folding. Exact-case matching is the conservative inference from the documented naming rule and the reported live failure; it is not an explicit case-sensitivity guarantee quoted from the docs. Our tests inspect the emitted literal rules and do not claim to test Claude Code's permission engine. The fix works whether Claude currently matches case-sensitively or adds case folding later.

## Specs and rollout

The base resident-agent permission requirement names connector products but no server spellings; this restores its existing reviewed-read/unknown-tool behavior. Per the bug-fix scope, no delta is needed. The resident-coordinator outward worker-deny derivation remains shared. There is no REST or frontend change for TUI, web or macOS.

BuildCmd behavior changes, so supervisor spawn surface advances to 49 with a refreshed digest. Install the binary, restart the supervisor (a daemon-only bounce is insufficient), and restart/resume the resident and any existing resident workers so each receives the expanded policy. A supervisor restart interrupts running sessions; coordinate deployment with the operator. This PR does not perform deployment.
