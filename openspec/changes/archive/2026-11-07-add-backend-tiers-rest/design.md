## Decisions

- **Usage mirrors routing, not the account projection.** The ticket's `usage` is the single reading `ResolveChoice` scores a tier with (`percent_used`, `resets_at`, `measured_at`, `scope`, `account`). Claude is per account (`scope:"account"`, account = tier account, else `default_account`, else `default`). The Codex reading is one global value and only scores a tier whose selected account is `default`, so it reports `scope:"global"`, `account:null`; a Codex tier whose selected account is named reports `null`, exactly as routing treats it as unread. Per-window usage from the account projection stays on `/api/accounts`.
- **Revision** is the first 16 hex chars of a SHA-256 over the ordered tier tuples. It ignores source, so a config.toml list with identical content has the same revision. Also sent as `ETag`.
- **Check order on PUT:** body shape (400) → config.toml ownership (409) → `If-Match` present (428) → matches (412) → whole-list validation (400) → store (500).
- **Authority:** any authenticated token, like other settings writes. Tiers carry no command templates or credential paths; backends and accounts stay master-only.
- **Serialization:** shares `accountWriteMu` so an account delete cannot interleave between validation and save.
- **Events:** the daemon watcher fingerprints the routed readings per tick; the first observation is a baseline.

## Deviations

- The ticket's usage example lacks a freshness rule; expired readings (`resets_at` in the past) are returned as null.
