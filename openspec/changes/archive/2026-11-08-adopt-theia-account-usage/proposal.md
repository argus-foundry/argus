# Show existing Claude account usage in Agents settings

Render each account's valid five-hour and weekly Claude usage with reset time through the existing accounts array read. Reuse the composer account-window validity policy. Invalid, absent or expired readings stay unavailable and never become zero.

## Non-Goals
No backend or account CRUD changes. Native follow-up `macos-agents-account-usage-settings` evaluates equivalent native settings presentation. Codex quotas remain outside this Claude-only contract. Current read-only account settings have no edit drafts to preserve.

## Abstraction inventory
| Name | Type | Responsibility | Depends on |
| --- | --- | --- | --- |
| Account usage window | Value | Valid Claude percentage and reset time for one account | Existing accounts endpoint |
| Account settings record | Visual Component | Display matching account identity and usage | AgentSettings |

Reuse existing accountWindows validity policy; no provider guesses or new actor.
