## Context

Argus runs Claude tasks under one of several configured accounts. Each account is a separate Claude Code config dir, for example `default` = `~/.claude` (work) and `personal` = `~/.claude-personal`. Tier routing (`backend-tier-routing`) and account moves (`account-move`) deliberately shift a task to whichever account has usage headroom. Claude Code's built-in artifact tool publishes through the session's own claude.ai login, so the artifact lands on whichever account the task happens to be running on. The owner's artifacts end up scattered across the personal and work organizations.

Constraints:

- An agent cannot publish as an account it is not signed into.
- The project's home account may be at 0% usage until its weekly reset. A "spawn a headless Claude on the home account to publish" relay therefore fails exactly when it is needed. Publishing must not require a model run.
- Argus's account design never reads another account's stored credentials (see `gotchas/misc.md`, add-agent-accounts: "advisory sign-in lookups never reading auth.json/credentials").

## Discovery findings

- Claude Code's artifact tool talks to an HTTP MCP endpoint on claude.ai (`/api/artifacts/mcp`; `/v1/code/mcp/ccr-artifacts` in remote sessions). It authenticates with the account's claude.ai login. A publish is an MCP tool call, not model inference, so it is expected to work while the account's model budget is exhausted. The `artpush` spike must confirm this.
- `[project_accounts]` already maps some projects to an account (`History = "personal"`). It is a routing *default*, not an ownership statement, but for projects that set it, it is the right artifact home.
- `BuildCmd` (`internal/agent/agent.go`) already exports task-scoped env (`ARGUS_TASK_ID`, `ARGUS_PROFILE`, …). It runs on every spawn, resume, kick and account-move restart, so an export there automatically follows a task across moves.
- The sandbox profile already allows `~/Library/Keychains` writes and outbound network, so `artpush` needs no new SBPL grant.
- The rule "never branch on backend identity outside `backends.go`" means "does this backend have the built-in artifact tool" must be a `Family` field.

## Goals / Non-Goals

**Goals:**

- Every claude.ai artifact produced in a mapped project lands on that project's home account, whichever account the task is routed to.
- Zero behavior change for unmapped projects, and for tasks already running on their home account.
- Argus decides which publish path an agent uses, in testable Go. The prose rule stays a trivial "if set, use artpush".

**Non-Goals:**

- Building the `artpush` CLI itself. It gets a separate repo and change. This change depends only on its command contract.
- Moving artifacts already published to the wrong account.
- Comments, live data, or watch subscriptions for artifacts published through `artpush`.
- A per-publish override of the home account.
- Editing `[project_artifact_accounts]` in the TUI, web or macOS Settings. It is config.toml-only for now. **Named follow-up:** add it to the TUI Settings Accounts category next to project accounts.

## Decisions

### D1. Publishing to another account goes through an external CLI with its own login

The `artpush` CLI calls the claude.ai artifacts endpoint directly, using its own OAuth token for the target account. It gets that token from a one-time `artpush login <account>` and keeps it in its own Keychain entry.

Alternatives considered:

- **Headless Claude relay** (spawn `claude -p` under the home account to publish). Rejected because it needs model budget on the home account, which is the exact failure case.
- **Borrow Claude Code's saved token.** Rejected because refreshing an expired token can rotate the refresh token and sign Claude Code out of that account. It also contradicts the never-read-credentials rule.
- **Keep artifact-producing projects on their home account.** Rejected because it loses usage-overflow routing.
- **After-the-fact sweep.** Rejected because it breaks links that were already shared.

### D2. Home account resolution: explicit map, then `project_accounts`, else none

`ArtifactAccount(project)` returns `ProjectArtifactAccounts[project]` when it is a valid Claude account. Otherwise it returns `ProjectAccounts[project]` when that is a valid Claude account. Otherwise it returns `""`. A valid Claude account is `default` or a configured account with a `claude_config_dir` (`AccountSupports` for a Claude command): artifacts live on a claude.ai login, so a `codex_home`-only account cannot be a home. An entry naming an unknown or non-Claude account is skipped with a log line, never fatal, mirroring `ResolveAccount`. The `default` account name is valid. It deliberately does **not** fall back to `default_account`: an unmapped project has no home account and keeps today's behavior.

### D3. Argus exports one variable, only when the agent must use artpush

`ARGUS_ARTIFACT_ACCOUNT=<home>` is exported when both of these hold:

1. the task's project has a home account, and
2. either the backend family lacks the built-in artifact tool, or the task's effective account differs from the home account.

Otherwise the variable is unset.

The alternative was exporting both `ARGUS_ARTIFACT_ACCOUNT` and `ARGUS_ACCOUNT` and having the agent compare them. That is rejected because it puts a family-dependent comparison into prose the agent can misapply, and leaves it untested. Exporting one conditional variable keeps the whole decision in Go.

### D4. New `Family.NativeArtifacts` capability

The flag is true for the `claude` family only. `custom` stays false: a custom template may not be Claude Code at all, and routing it through artpush is harmless when the CLI is present.

### D5. Routing snippet `argus-artifacts.md`, self-gating like the other builtins

The snippet says: if `ARGUS_ARTIFACT_ACCOUNT` is unset, ignore this section. If it is set, publish or update claude.ai artifacts with `artpush publish <path> --account "$ARGUS_ARTIFACT_ACCOUNT"`, or `--update <url>` for an artifact that already exists, instead of the built-in tool. Return the printed link. If artpush reports a missing login, tell the user to run `artpush login <account>`. Do not fall back to the built-in tool, because that recreates the scattering.

## Risks / Trade-offs

- **The undocumented claude.ai endpoint changes shape** → artpush fails loudly with a non-zero exit and a clear message, and the agent surfaces it. No silent fallback to the wrong account.
- **A publish turns out to need model budget** → the artpush spike verifies this before the Argus side ships. If it does need budget, D1 is revisited before implementation.
- **artpush is not installed or not logged in** → the agent reports the CLI's error to the user. Argus does not check for artpush at spawn: that would mean a new doctor-style check, out of scope, and a possible follow-up.
- **Two publish paths for edits** → a project's artifacts always live on its home account. An edit made while routed away goes through artpush `--update`, and an edit made on the home account uses the built-in tool. Both target the same artifact.
- **Cursor and custom backends get the variable but no guidance** → neither family has a routing-delivery mechanism, so the `argus-artifacts` snippet never reaches them. They still receive `ARGUS_ARTIFACT_ACCOUNT` (harmless, and usable by a project skill). Known gap; adding routing delivery to those families is out of scope.
- **An env var is fixed for the session's lifetime** → account moves restart the session (kick), so `BuildCmd` re-runs and the variable is recomputed.

## Migration Plan

This change is additive. Rollback means removing the config table: with no table and no `project_accounts`, no variable is exported. To roll out:

1. Install artpush and log in to each account.
2. Add `[project_artifact_accounts]` entries.
3. Restart the supervisor so new spawns pick up the export.

## Open Questions

None for the Argus side. The artpush feasibility questions (no model budget needed, third-party OAuth login) belong to the artpush change and gate this one's rollout, not its design.

## Acceptance criteria

**Home account resolution**

- It should resolve a project's home account from `[project_artifact_accounts]` when present.
- It should fall back to the project's `[project_accounts]` entry when no artifact entry exists.
- It should resolve no home account for a project in neither table.
- It should ignore an entry naming an unknown account, log it, and continue with the fallback.

**Env export**

- It should not export `ARGUS_ARTIFACT_ACCOUNT` when the Claude task runs on its home account.
- It should export `ARGUS_ARTIFACT_ACCOUNT=<home>` when the Claude task runs on another account.
- It should export `ARGUS_ARTIFACT_ACCOUNT=<home>` for a non-Claude backend task in a mapped project.
- It should not export it for any task in an unmapped project.
- It should recompute the export after an account move, so a task moved onto its home account no longer sees it.

**Routing guidance**

- The injected routing content should name `artpush publish` with `--account "$ARGUS_ARTIFACT_ACCOUNT"` and the `--update` form, and tell the agent not to fall back to the built-in tool.
