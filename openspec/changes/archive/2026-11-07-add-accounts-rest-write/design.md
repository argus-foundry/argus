## Decisions

- **Ownership is per account, not per field.** `config.toml` decoding replaces a whole `[accounts.<name>]` table, so `source` is uniform across the three fields (the ticket's mixed example cannot occur). A `config_file` account is read-only (409).
- **Delete refuses instead of cascading.** The TUI clears default/project references on delete; the API reports them in a 409 so a web client cannot silently rewrite defaults.
- **Single-account responses omit sign-in and usage lookups** (identity and source only) so a write never waits on the 3s sign-in probes.
- Validators moved to `config.ValidateNewAccountName` / `ValidateAccountEdit`, shared with Settings, in a new file outside the supervisor spawn manifest.
- Built-in `default`: 400 on PATCH/DELETE/POST (not 409), since it is not a config.toml ownership conflict.
