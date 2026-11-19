## MODIFIED Requirements

### Requirement: Resident permission requests become durable decisions

The system SHALL intercept supported resident PermissionRequest events, store the exact tool and canonical input as a human board decision, and deny the current attempt with a model-visible queued explanation and interrupt:false. Repeated identical pending attempts SHALL reuse the decision, and the reply SHALL say the call is already queued. A new request whose content matches older pending requests (same task, cwd, tool and canonical input, ignoring launch and session) SHALL supersede them: they close with the note "superseded by #N", and the reply names them. The queued reply SHALL tell the resident to do other work, never to retry or route around the approval, how to wait for the decision when Aaron is present (`hera_inbox` with a timeout), and that it can withdraw an item it no longer needs. Ordinary tasks SHALL retain existing hooks. Native deny/ask policy SHALL remain unchanged.

#### Scenario: Connector write waits while Solo continues

- **WHEN** Solo requests a connector write or Bash call requiring native permission
- **THEN** its current attempt does not execute, a durable board item identifies the exact call, and Solo receives instructions to continue other work until an `[argus] ` decision notice arrives

#### Scenario: Re-sending an identical call supersedes the old item

- **WHEN** #562 is pending from an earlier launch or session and Solo sends the identical call again
- **THEN** a new pending item #574 is queued, #562 closes with the note "superseded by #574", and the reply says #574 replaces #562

#### Scenario: Re-sending while still pending on the same launch

- **WHEN** Solo re-sends a call that is still pending on the current launch
- **THEN** no new item is created and the reply says the call is already queued as that item

#### Scenario: Hook unavailable

- **WHEN** the hook cannot authenticate, parse its bounded input or reach the daemon
- **THEN** the call is denied and never silently approved

#### Scenario: Other native dialogs

- **WHEN** PermissionRequest concerns a tool other than Bash or an MCP tool
- **THEN** both the hook matcher and binary leave the dialog to native behavior without queuing a decision

### Requirement: Human approval grants only an exact retry once

The TUI SHALL show the stored tool and input with Approve once and Deny actions. Only an explicit human approval action SHALL create a grant; agent board edits, reads, comments, withdrawal and resolution SHALL NOT authorize execution. Approval SHALL expire 60 minutes after Aaron approves and SHALL be consumed atomically by one matching retry for the same resident launch, session, cwd and tool/input. Changed launches SHALL invalidate outstanding grants. Deny rules SHALL never be weakened. Board item text and decision notices SHALL state these rules accurately: a pending item does not time out, the 60-minute window starts at approval, and a relaunch before use voids the item.

#### Scenario: Approved matching call

- **WHEN** Aaron approves a request and Solo retries exactly that call within 60 minutes in the same launch
- **THEN** PermissionRequest returns allow once without rewriting input or permission rules, and the board item's note records when the call ran

#### Scenario: Mismatch or duplicate

The system SHALL exclude only Bash's model-written description from canonical matching, retain every execution field, and include the stored canonical call in approval notices.

#### Scenario: Bash description changes after compaction

- **WHEN** a retry changes only the Bash description after human approval
- **THEN** it consumes the original grant once; a changed command or execution option remains unauthorized

#### Scenario: Execution identity mismatch

- **WHEN** input, tool, session, cwd or launch differs, a grant expired, or a concurrent retry already consumed it
- **THEN** no existing grant authorizes the call

#### Scenario: Denial or forged board resolution

- **WHEN** Aaron denies a request or an agent resolves or withdraws its board item
- **THEN** no grant is created and no call executes as a result

## ADDED Requirements

### Requirement: Approval items close when they can no longer act

The daemon SHALL check resident approvals on every resident manager tick and at startup. A pending or approved item recorded under a launch other than the current one SHALL become `stale`, with the board note "lapsed: relaunched before use". An approved item past its window SHALL become `expired`, with the note "approved, expired unused". Each lapse SHALL resolve the board item and, in the same transaction, queue one `[argus] ` notice to the requesting role naming the item and its reason, carrying the stored canonical call, and saying nothing ran and the call can be re-sent if it is still needed. A consumed, denied, withdrawn or superseded item SHALL never be announced again.

#### Scenario: Relaunch voids a pending item

- **WHEN** #562 is pending and Solo is relaunched by an account move
- **THEN** #562 is resolved with a "lapsed" note and Solo receives one `[argus]` notice telling it to re-send the call if still needed

#### Scenario: Approved grant goes unused

- **WHEN** Aaron approves #574 and Solo does not retry within 60 minutes
- **THEN** #574's note becomes "approved, expired unused" and Solo is told the approval expired without running

#### Scenario: Lapse is announced once

- **WHEN** the sweep runs again after an item lapsed
- **THEN** no further notice is queued for it

### Requirement: Approval state is visible and decide errors are specific

Board items SHALL expose `approval_state` (`pending`, `approved`, `consumed`, `denied`, `stale`, `expired`, `withdrawn` or `superseded`; empty for ordinary items) in every listing, including `hera_board_list`. Approve and deny SHALL resolve the item immediately with a note naming the decision and time. A decide attempt on an item that is no longer pending SHALL fail with a message for its actual state. The messages SHALL distinguish already approved (with the approval time and the retry deadline, waiting for the agent to run it), already used (with the run time), denied, lapsed after a relaunch (ask the agent to re-send), expired unused, withdrawn, superseded (naming the replacement item), and changed since it was opened. These errors SHALL still match the generic approval error for callers that test for it. The TUI SHALL show that message instead of opening the confirmation dialog for an item that is no longer pending.

#### Scenario: Re-clicking approve

- **WHEN** Aaron approved #574 at 17:07 and presses approve on it again
- **THEN** the error says it was already approved at 17:07 and is waiting for the agent to run it, and no second notice is sent

#### Scenario: Approving a lapsed item

- **WHEN** Aaron presses approve on an item recorded before the latest relaunch
- **THEN** the error says the agent was relaunched and should be asked to re-send the call

#### Scenario: Board state right after approval

- **WHEN** Aaron approves an item and the resident immediately calls `hera_board_list` with `include_resolved`
- **THEN** the item shows `resolved: true`, `approval_state: approved` and a note with the approval time

#### Scenario: Board state right after denial

- **WHEN** Aaron denies an item
- **THEN** the item shows `resolved: true`, `approval_state: denied` and a note with the denial time

### Requirement: The resident can withdraw its own pending approval

An agent `hera_board_resolve` on its own pending approval item SHALL mark it `withdrawn`, resolve the board item with the agent's note, and never create a grant. For an approval item in any other state it SHALL return the state-specific message. Generic human dismissal SHALL NOT decide approval items; approve and deny remain the only human actions.

#### Scenario: Withdraw a dead request

- **WHEN** Solo calls `hera_board_resolve` on its pending item #562 with the note "superseded"
- **THEN** #562 is resolved as `withdrawn` and approving it later reports that it was withdrawn

#### Scenario: Withdrawing a decided item

- **WHEN** Solo calls `hera_board_resolve` on an item Aaron already approved
- **THEN** the call fails with the already-approved message and the grant is unchanged
