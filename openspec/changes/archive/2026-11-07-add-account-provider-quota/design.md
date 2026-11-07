## Decisions

- **Projection lives in `internal/backendtier`** (`usageprojection.go`) so the tier editor (ENG-1047) and the API share one function; API code only marshals it.
- **Window kinds** are `five_hour` (300 min) and `seven_day` (10080 min). Codex windows with any other length are ignored, matching the existing weekly-by-duration rule. An expired window (`resets_at <= now`) is omitted; no valid window means `usage: null`, never zero.
- **Codex is account-scoped**: the reader scans `<codex_home>/sessions` of that account (default home `~/.codex`) and caches by file path+mtime+size. The global routing snapshot is never attached to named accounts.
- **Deviation: identity** — the ticket asks for Codex email/plan. Email/organization live only in `auth.json`, which the add-agent-accounts gotcha forbids reading, so they stay null. `plan` is taken from the rollout `rate_limits.plan_type` field when present.
- **`provider`** is `anthropic`/`openai` for a supported account, and null when unsupported or when the tool's backend `env_vars` re-route it (`ANTHROPIC_BASE_URL`, Bedrock/Vertex switches, `OPENAI_BASE_URL`).
- **Event** `account.usage_changed` is emitted by a daemon watcher that fingerprints each account/provider projection every 30s and on start; the first observation is a baseline, not an event.
