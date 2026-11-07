## ADDED Requirements

### Requirement: Account provider identity and quota over REST

`GET /api/accounts` SHALL include, for every account, a `providers` object with `claude` and `codex` entries. Each entry carries `provider` (`anthropic`/`openai`, or null when unsupported or re-routed), `supported`, `logged_in` (null when unknown), `identity` (`email`, `plan`, `organization`, each null when unknown) and `usage` (null, or `measured_at` plus `windows[]` of `kind` `five_hour`/`seven_day`, `percent_used`, `resets_at`). Expired windows SHALL be omitted and absent data SHALL be null, never zero. Codex usage SHALL be read from the account's own Codex home only. The response SHALL NOT contain credentials, and GET SHALL NOT launch a probe.

#### Scenario: Codex short and weekly windows
- **WHEN** an account's newest Codex rollout reports a 300-minute and a 10080-minute window, both unexpired
- **THEN** `providers.codex.usage.windows` lists `five_hour` and `seven_day` with their reset times

#### Scenario: Expired or missing data
- **WHEN** every window has reset, or no rollout exists
- **THEN** `providers.codex.usage` is null

#### Scenario: Mixed provider account
- **WHEN** an account has Claude usage only
- **THEN** `providers.codex` carries no Claude plan or quota

### Requirement: Account usage change event

The daemon SHALL emit `account.usage_changed` with payload `{"account","kind","measured_at"}` when an account/provider's projected usage changes, carrying no usage values.

#### Scenario: Reading changes
- **WHEN** an account's Codex usage projection differs from the last observed one
- **THEN** one event is emitted for that account and kind
