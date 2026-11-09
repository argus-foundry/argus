# account-move Specification

## Purpose

Move a running Claude task's conversation to another account in place, so an agent stuck on one account's usage limit can continue on an account with budget.
## Requirements
### Requirement: A Claude task can be moved to another account

The system SHALL provide an account-move operation for a task on a Claude backend. The target SHALL be a configured account, `default`, or `auto`. For `auto` the system SHALL choose, among the accounts that support the backend (`default` first, then configured names in sorted order) excluding the task's current account, the one with the lowest current Claude usage, where an account's usage is the fuller of its currently valid 5-hour and weekly readings and an account with no valid reading counts as 0%, ties going to candidate order.

The move SHALL be rejected without side effects when:

- the task does not exist, or its backend is not Claude;
- the target is unknown, equals the current account, or does not support the backend;
- the target is `auto` and there is no other candidate;
- the task's session is live and not idle, and the caller did not request force;
- another move or restart of the task is already in progress (force does not override this).

The operation SHALL be carried out entirely by Argus and SHALL NOT need any agent turn or model usage on the task's current account. It SHALL NOT create a new task: the same task row, worktree, branch and hera bindings are kept, and only the agent process is replaced.

The system SHALL:

1. Ensure the target's Claude config dir exists.
2. Copy the task's newest transcript (`<session-id>.jsonl`, plus its `<session-id>/` sidecar directory when present) from the source account's `projects/<encoded worktree>/` into the target's, overwriting an older copy and leaving the source copy in place.
3. Store the target as the task's account, clearing the routed flag. Only the account and routed flag SHALL be written.
4. Stop a live session and resume it under the target account in place.

When no transcript exists, the move SHALL still complete, the session SHALL start fresh, and the result SHALL report that the conversation was not resumed. When the restart fails while the source session is still running, the previous account and routed flag SHALL be restored and the move reported as failed. Every move SHALL be logged under `[account]` with the task, source and target.

#### Scenario: Stuck agent moved and resumed
- **WHEN** an idle task on `default`, whose session hit its 5-hour cap, is moved to `personal`
- **THEN** its transcript is copied into `personal`'s projects dir, the task's account becomes `personal`, and the session resumes the same conversation under `personal`

#### Scenario: Move to auto
- **WHEN** a task on `personal` is moved with target `auto`, `default` is at 40%, and `work` is at 85%
- **THEN** the task moves to `default`

#### Scenario: Same task, same hera role
- **WHEN** a hera worker on `default` is moved to `personal`
- **THEN** no new task is created, the task keeps its ID, worktree, branch and worker binding, and its coordinator's messages still reach it

#### Scenario: Busy session refused without force
- **WHEN** a task's session is actively producing output and force is not requested
- **THEN** the move is rejected and nothing changes

#### Scenario: Busy session moved with force
- **WHEN** a task's session is actively producing output and force is requested
- **THEN** the session is stopped and the move proceeds

#### Scenario: Failed restart rolls back
- **WHEN** a move's in-place restart is refused while the source session keeps running
- **THEN** the task's stored account and routed flag are put back and the move reports failure

#### Scenario: Non-Claude task refused
- **WHEN** a codex task is moved
- **THEN** the move is rejected naming the backend

#### Scenario: Moving back keeps history
- **WHEN** a task is moved from `default` to `personal` and later back to `default`
- **THEN** it resumes the newest conversation, including turns taken on `personal`

### Requirement: Account usage for moves survives a daemon bounce

The TUI SHALL read per-account usage for the `>` picker and the `auto` target over a fresh daemon connection when its long-lived client has failed, and SHALL treat usage as unknown only when the daemon is unreachable on that fresh connection too.

#### Scenario: Daemon bounced since the TUI started

- **WHEN** the TUI's original daemon connection is dead but the daemon is running
- **THEN** the picker and `auto` use the daemon's current per-account usage readings

### Requirement: The move picker lists every account and allows re-picking the current one

The TUI `>` picker SHALL list `auto` plus every account that supports the task's backend, including the account the task is on, which SHALL be marked as current. Each row SHALL show the account's 5-hour and weekly usage and SHALL warn when either window is at or above 80%; a warned account SHALL remain selectable. A move whose target is the task's current account SHALL restart the task in place on that account (resuming its conversation when a transcript exists), copy nothing, and pin the account.

#### Scenario: Current account is listed and marked

- **WHEN** a task on `default` opens the picker and `personal` also exists
- **THEN** the rows are `auto`, `default` (marked current) and `personal`, each with usage

#### Scenario: Re-picking the current account restarts in place

- **WHEN** a live task on `default` is moved to `default`
- **THEN** the session restarts in place on `default` with the conversation resumed, and the task is pinned to `default`

### Requirement: A task can be moved to another CLI by a restart with handoff

The system SHALL let an account-move request name a target `backend` (a configured backend of a different family, Claude or Codex) and, optionally, a target account that supports it (default `default`). This is a restart in place, NOT a resume: the conversation SHALL NOT be carried over, and every surface and result SHALL say so.

The move SHALL be rejected without side effects when the task does not exist, the current or target backend is not Claude/Codex, the target backend is the same family as the current one, the target account does not support the target backend, the session is live and not idle without force, or another move or restart is in progress.

Otherwise the system SHALL, without any model turn: build a handoff seed; persist the target backend and account, clear the model and session id, and store the seed as the task prompt (keeping the first original prompt for later seeds); and restart a live session in place with the seed as its opening prompt. The task row, worktree, branch and hera bindings SHALL be kept. When the restart fails while the old session still runs, every written field SHALL be restored.

The seed SHALL state that the task moved between CLIs and the prior conversation did not carry over, and SHALL include the worktree, branch, `git status`, the original task prompt, and the last human exchange when a Claude transcript is readable. Any read failure SHALL omit that part and never fail the move.

#### Scenario: Claude task moved to Codex
- **WHEN** an idle Claude task is moved to backend `codex`
- **THEN** its backend and account change, its session id is cleared, and a Codex session starts in the same worktree with the handoff seed

#### Scenario: Unreadable transcript
- **WHEN** the Claude transcript is missing
- **THEN** the move still completes and the seed omits the last exchange

#### Scenario: Restart failure rolls back
- **WHEN** the restart fails while the Claude session still runs
- **THEN** backend, model, account, session id and prompt are restored

#### Scenario: Same family rejected
- **WHEN** the target backend is another Claude backend
- **THEN** the move is rejected and the task is unchanged

### Requirement: The picker offers handoff rows

The TUI `>` picker SHALL list, after the same-CLI rows, one row per target backend and supporting account for the other CLI, labelled as a restart with handoff (conversation not carried over), with Codex usage where a reading exists. The label or help, the MCP description and the README SHALL name the losses on Codex: no bridge mod or context indicator, scraper-based needs-input detection, and `hera_new_orchestrator` rejected.

#### Scenario: Claude task sees a Codex handoff row
- **WHEN** a Claude task opens the picker and a Codex backend exists
- **THEN** a row labelled `Codex · default` marked as a restart with handoff (conversation not carried over) follows the account rows, and a Codex task sees the matching Claude rows

