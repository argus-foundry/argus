## ADDED Requirements

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
