## ADDED Requirements

### Requirement: Resident permission requests become durable decisions

The system SHALL intercept supported resident PermissionRequest events, store the exact tool and canonical input as a human board decision, and deny the current attempt with a model-visible queued explanation and interrupt:false. Repeated identical pending attempts SHALL reuse the decision. Ordinary tasks SHALL retain existing hooks. Native deny/ask policy SHALL remain unchanged.

#### Scenario: Connector write waits while Solo continues

- **WHEN** Solo requests a connector write or Bash call requiring native permission
- **THEN** its current attempt does not execute, a durable board item identifies the exact call, and Solo receives instructions to continue other work until an `[argus] ` decision notice arrives

#### Scenario: Hook unavailable

- **WHEN** the hook cannot authenticate, parse its bounded input or reach the daemon
- **THEN** the call is denied and never silently approved

### Requirement: Human approval grants only an exact retry once

The TUI SHALL show the stored tool and input with Approve once and Deny actions. Only an explicit human approval action SHALL create a grant; agent board edits, reads, comments and resolution SHALL NOT authorize execution. Approval SHALL expire after ten minutes and SHALL be consumed atomically by one matching retry for the same resident launch, session, cwd and tool/input. Changed launches SHALL invalidate outstanding grants. Deny rules SHALL never be weakened.

#### Scenario: Approved matching call

- **WHEN** Aaron approves a request and Solo retries exactly that call before expiry in the same launch
- **THEN** PermissionRequest returns allow once without rewriting input or permission rules

#### Scenario: Mismatch or duplicate

- **WHEN** input, tool, session, cwd or launch differs, a grant expired, or a concurrent retry already consumed it
- **THEN** no existing grant authorizes the call

#### Scenario: Denial or forged board resolution

- **WHEN** Aaron denies a request or an agent resolves its board item
- **THEN** no grant is created and no call executes as a result

### Requirement: Decisions notify Solo durably without waiting state

Human decisions SHALL atomically queue a reliable system notice beginning `[argus] `. Deferred board attention SHALL NOT count as a pending terminal prompt or prevent an otherwise quiet automatic account move. The transient dialog while a supported hook responds SHALL NOT raise ResidentWaiting. Unsupported sandbox network and mandatory connector consent prompts SHALL retain native behavior.

#### Scenario: Human answers while Solo is busy

- **WHEN** Aaron approves or denies during another resident turn
- **THEN** the decision is durable and its system notice is delivered when Solo can receive it

#### Scenario: Dialog flashes during hook

- **WHEN** a resident permission dialog renders briefly before the hook returns its answer
- **THEN** the hook-handled dialog does not leave Solo flagged as waiting for input
