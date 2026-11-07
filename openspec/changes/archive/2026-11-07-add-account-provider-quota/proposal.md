## Why

REST clients cannot see Codex quota, plan or sign-in identity, and nothing labels which provider executes an account's tools. The web workspace and TUI task details need both (ENG-1038).

## What Changes

- `GET /api/accounts` adds a `providers` object (`claude`, `codex`) to every account: `provider`, `supported`, `logged_in`, `identity{email,plan,organization}` and `usage{measured_at,windows[]}`.
- A reusable provider-usage projection in `internal/backendtier` (`ProviderUsage`, `ClaudeProviderUsage`, `CodexHomeUsage`) is the single place usage readings become windows; the backend tier editor reuses it.
- Codex quota is read per account from `<codex_home>/sessions` rollout `rate_limits` records (short 300-minute and weekly 10080-minute windows). No auth files are read.
- New SSE event `account.usage_changed` `{"account","kind","measured_at"}`, emitted by a change-detecting daemon watcher.
- Existing flat Claude fields stay.

## Non-Goals

- No Web, TUI or macOS UI. Follow-ups: ENG-1079 (Web: show the selected provider account and quota), ENG-1092 (TUI: inspect context, quota and session tokens).
- No paid probes from GET; no Codex PTY `/status` probe for named accounts.
- No Codex email/organization: they exist only in credential files, which Argus never reads. They stay null.
