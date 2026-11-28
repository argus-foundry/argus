## Why

Repository paths can be redirected and merge decisions depend on the configured default ref. The task API also omits false archival flags.

## What Changes

- Use the captured canonical repository directory for resident stack and lease operations.
- Scope TUI stack verdicts to the repository and merge target.
- Interpret an omitted archive flag as false.
