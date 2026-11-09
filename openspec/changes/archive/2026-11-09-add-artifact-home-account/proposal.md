## Why

Usage-based account routing moves Claude tasks between the personal account and the default (work) account. Claude Code's built-in artifact publish tool always publishes to the account the session is signed into, so personal artifacts land on the work account and work artifacts land on the personal account. An agent cannot publish to an account it is not signed into, and that account may have no usage left. A model run under the home account is therefore not a reliable relay either.

## What Changes

- New `[project_artifact_accounts]` config map naming each project's **artifact home account**. A project with no entry there falls back to its `[project_accounts]` entry. With neither, the project has no artifact home account and nothing changes.
- At every agent spawn or restart, Argus compares the task's artifact home account with the account the session will actually run on. When the two differ, or the backend has no built-in claude.ai artifact tool (Codex, Pi, OpenCode, etc.), Argus exports `ARGUS_ARTIFACT_ACCOUNT=<home account>`. Otherwise it leaves the variable unset.
- A new builtin routing snippet tells agents what to do with that variable. When it is set, publish or update claude.ai artifacts with the external `artpush` CLI (`artpush publish … --account $ARGUS_ARTIFACT_ACCOUNT`). When it is unset, keep using the built-in artifact tool.
- A new backend family capability flag records whether a backend has Claude Code's built-in artifact tool.
- **Dependency, not part of this change:** the `artpush` CLI lives in its own repository and has its own change. It provides a one-time per-account login with its own Keychain token, `publish` and `--update`, and loud failures.

## Capabilities

### New Capabilities

- `artifact-home-account`: how a project's artifact home account is resolved, when Argus exports `ARGUS_ARTIFACT_ACCOUNT` to an agent, and the routing guidance that steers agents to the `artpush` CLI only when needed.

### Modified Capabilities

(none)

## Impact

- `internal/config`: new `ProjectArtifactAccounts` map, a resolver, and config.toml override parsing for the new table.
- `internal/agent`: the env export in `BuildCmd` (`agent.go`), plus a new `Family` capability field in `backends.go`. The supervisor spawn surface digest must be re-recorded.
- `internal/routing/builtin/`: a new `argus-artifacts.md` snippet.
- Docs: a `context/knowledge/gotchas/misc.md` bullet and the README Reference config table.
- No REST, TUI, web, or macOS change. The table is config.toml-only for now, and Settings UI editing is a named follow-up.
- External: needs the `artpush` binary on `PATH` and a one-time `artpush login <account>` for each account.
