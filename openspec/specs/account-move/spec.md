# account-move Specification

## Purpose

Move a running Claude or Codex task's conversation to another account in place, so an agent stuck on one account's usage limit can continue on an account with budget.

## Requirements

### Requirement: A task can be moved to another account

The system SHALL provide an account-move operation for a task whose backend family declares a conversation transfer (Claude and Codex). The target SHALL be a configured account, `default`, or `auto`. For `auto` the system SHALL choose, among the accounts that support the backend (`default` first, then configured names in sorted order) excluding the task's current account, the one with the lowest current Claude usage, where an account's usage is the fuller of its currently valid 5-hour and weekly readings and an account with no valid reading counts as 0%, ties going to candidate order.

The move SHALL be rejected without side effects when:

- the task does not exist, or its backend family has no conversation transfer;
- the target is unknown, equals the current account, or does not support the backend;
- the target is `auto` and there is no other candidate;
- the task's session is live and not idle, and the caller did not request force;
- another move or restart of the task is already in progress (force does not override this);
- the target Codex account is not signed in or has no `state_5.sqlite`.

The operation SHALL be carried out entirely by Argus and SHALL NOT need any agent turn or model usage on the task's current account. It SHALL NOT create a new task: the same task row, worktree, branch and hera bindings are kept, and only the agent process is replaced.

The system SHALL:

1. Ensure the target's account home exists.
2. Copy the task's newest transcript (`<session-id>.jsonl`, plus its `<session-id>/` sidecar directory when present) from the source account's `projects/<encoded worktree>/` into the target's, overwriting an older copy and leaving the source copy in place.
3. Store the target as the task's account, clearing the routed flag. The account, routed flag and captured or fresh-launch session identity SHALL be written together without overwriting other task columns.
4. Stop a live session and resume it under the target account in place.

For a Codex task, step 2 SHALL instead copy the task's thread row (identity and folder columns excluded) into the target's `state_5.sqlite` and its rollout file under the target home's `sessions/`, leaving the source and all credentials untouched; a missing source thread or rollout SHALL complete the move without resuming. The target thread database SHALL be validated before this fallback. An empty saved Codex session ID SHALL use account-scoped thread discovery before transfer, with uncertain discovery refused. Missing-source moves SHALL clear stale capture-style IDs and start fresh on both live and stopped paths. Source rollout reads SHALL stay inside canonical source sessions storage. The source sessions anchor and every component below it SHALL be non-symlink entries; opened directories and files SHALL match their checked identities. Destination path components SHALL refuse symlinks using rooted operations, and publication SHALL use the checked parent directory handle. The opened source rollout SHALL have exactly one filesystem link. Before copying, its first JSONL record, capped at 4 MiB including the newline for moves, SHALL be native session_meta with matching indexed thread id, indexed cwd and a creation timestamp. Invalid or multiply linked files SHALL be refused with the account error before destination writes. Context readers SHALL retain their existing 64 KiB default. The validated descriptor SHALL be rewound so the whole rollout is copied. Physical home/database aliases SHALL be rejected. Index-write failure SHALL preserve the previous destination file and row. Source credentials and non-allowlisted target metadata SHALL remain unchanged. Canceled HTTP moves SHALL leave a queued task lock without later side effects.

When no transcript exists, the move SHALL still complete, the session SHALL start fresh, and the result SHALL report that the conversation was not resumed. When a kick returns an error, the target account and routed flag SHALL remain persisted and the move SHALL report an unknown restart outcome, without inferring rollback or a second launch from a liveness read. Every move SHALL be logged under `[account]` with the task, source and target.

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

#### Scenario: Failed kick keeps the target
- **WHEN** a kick returns an error even though the source handle looks alive
- **THEN** the target stays persisted, restart_unknown is returned, and no second launch is attempted

#### Scenario: Family without a transfer refused
- **WHEN** a task on a backend family with no transfer (pi, opencode, antigravity, custom) is moved
- **THEN** the move is rejected naming the backend

#### Scenario: Codex task moved and resumed
- **WHEN** a Codex task on `default` is moved to a signed-in Codex account
- **THEN** the thread and rollout exist under the target home, the source copy remains, no credentials are copied, and the session resumes the same conversation

#### Scenario: Codex target not ready
- **WHEN** the target Codex account is not signed in or has no state database
- **THEN** the move is rejected and the task's account and session ID are unchanged

#### Scenario: Moving back keeps history
- **WHEN** a task is moved from `default` to `personal` and later back to `default`
- **THEN** it resumes the newest conversation, including turns taken on `personal`

#### Scenario: First live Codex move
- **WHEN** a live task has an empty saved ID and its account index identifies its thread
- **THEN** that thread is copied and its captured ID is persisted with the target account

#### Scenario: Unready target with missing source data
- **WHEN** the source ID, row or rollout is missing and the target index is missing or unusable
- **THEN** the move is rejected without account or session changes

#### Scenario: Missing conversation starts fresh
- **WHEN** a ready target accepts a task whose saved conversation is absent
- **THEN** stopped and live launch paths start fresh without resuming the missing ID

#### Scenario: Unsafe paths or physical account aliases
- **WHEN** the source rollout escapes sessions storage, a destination component is a symlink, or distinct account names share physical home identity
- **THEN** the transfer is refused and source storage remains unchanged

#### Scenario: SQLite write rejects the transfer
- **WHEN** a real target insert or update fails, including a trigger rejecting after a row change
- **THEN** the previous target row and rollout are restored, or no new row or rollout remains

#### Scenario: Canceled move waiting for its task lock
- **WHEN** an HTTP move is canceled before obtaining the task lock
- **THEN** it returns without waiting for the previous operation to finish and performs no later mutation

#### Scenario: Source sessions aliases credentials
- **WHEN** sessions points to . and the indexed rollout is sessions/auth.json
- **THEN** the move is rejected without copying credentials or changing either index or the selected account

#### Scenario: Credentials hard-linked at a rollout path
- **WHEN** the source rollout is a hard link to auth.json
- **THEN** the move is refused without copying credential bytes or changing account state or indexes

#### Scenario: Copied credentials at a rollout path
- **WHEN** a source rollout has one link but contains a copied credential JSON document
- **THEN** session_meta validation refuses the move before writing the target

#### Scenario: Legitimate rollout
- **WHEN** a singly linked rollout has valid metadata for the indexed thread
- **THEN** the entire file, including its header, is copied

#### Scenario: Large native instructions
- **WHEN** matching session metadata contains base_instructions.text larger than 64 KiB but fits the move budget
- **THEN** the move succeeds and copies the entire header and following records byte-for-byte

#### Scenario: Move metadata exceeds the resource budget
- **WHEN** the first JSONL record exceeds 4 MiB
- **THEN** the move is refused and task, files and indexes remain unchanged

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

