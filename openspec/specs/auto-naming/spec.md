# Task Auto-Naming

## Purpose

Newly created tasks start with a regex-derived slug taken from the prompt, which is often terse or awkward. Auto-naming asks the task’s resolved backend (Claude Haiku or low-cost Codex Luna) for a clearer, human-readable name and applies it to the task in the background. The process is fire-and-forget and fail-open: any failure leaves the original slug untouched, and it never clobbers a name the user changed in the meantime.
## Requirements
### Requirement: Replace the auto-generated slug with an LLM-suggested name

The system SHALL request a task name from the LLM for the given prompt and, when a different valid name is returned, persist it as the task's name. The system SHALL NOT run auto-naming for a task that was created with a user-supplied name (auto-naming is disabled at creation when the user named the task), so an explicitly chosen name is never replaced by an LLM suggestion.

#### Scenario: LLM returns a better name

- **WHEN** auto-naming runs for a task whose current name is the original auto-generated slug and the LLM returns a different non-empty name
- **THEN** the task's persisted name is updated to the LLM-returned name

#### Scenario: LLM returns the same name

- **WHEN** the LLM returns a name equal to the original slug
- **THEN** no rename occurs and the task's name is left unchanged

#### Scenario: Task created with a user-supplied name

- **WHEN** a task is created with an explicit user-supplied name (auto-naming disabled at creation)
- **THEN** auto-naming does not run and the task keeps the user-supplied name

### Requirement: Fail open on any LLM error

The system SHALL keep the original auto-generated slug whenever the LLM call
fails, and SHALL NOT propagate the error to the caller. When the LLM CLI exits
non-zero, the system SHALL include the CLI's emitted failure reason in the
logged error regardless of whether the CLI wrote it to stdout or stderr, so the
cause (e.g. budget exceeded, usage/rate limit, overload) is diagnosable rather
than a bare exit-status code.

#### Scenario: LLM call returns an error

- **WHEN** the LLM name request returns an error
- **THEN** the task's name remains the original slug and the operation completes without surfacing the error

#### Scenario: LLM backend unavailable

- **WHEN** the LLM backend is unavailable
- **THEN** auto-naming is skipped and the task's name remains the original slug

#### Scenario: Prompt is empty

- **WHEN** the prompt passed to auto-naming is empty
- **THEN** auto-naming is skipped and the task's name remains unchanged

#### Scenario: CLI writes its failure reason to stdout

- **WHEN** the LLM CLI exits non-zero and wrote its failure reason to stdout (with stderr empty)
- **THEN** the wrapped error includes that stdout reason instead of only a bare exit-status code

### Requirement: Never overwrite an externally changed name

The system SHALL apply the LLM-suggested name only if the task's current name still equals the original slug it started from, so a name changed externally while the LLM call was in flight is preserved.

#### Scenario: User renames during the LLM call

- **WHEN** the task is renamed by the user before the LLM returns, so the current name no longer equals the original slug
- **THEN** the LLM-suggested name is not applied and the user's chosen name is preserved

### Requirement: Tolerate a deleted task

The system SHALL handle the case where the task no longer exists when the LLM result is ready, completing without error and without writing any name.

#### Scenario: Task deleted before the LLM returns

- **WHEN** the task has been deleted before the LLM-suggested name is ready to apply
- **THEN** the operation completes without panic and no name is written

### Requirement: Bound the LLM call with a timeout

The system SHALL invoke the LLM name request under a context with a deadline so
the operation cannot run unbounded. The deadline SHALL be sized comfortably
above the observed `claude` CLI cold-start latency under machine load, so a
cold start that coincides with concurrent work is not killed before the model
replies. Because the request runs in a fire-and-forget background goroutine, a
generous deadline carries no user-facing cost.

#### Scenario: LLM call receives a deadline-bound context

- **WHEN** auto-naming invokes the LLM name request
- **THEN** the request is given a context that carries a deadline and the overall operation does not exceed the configured timeout

#### Scenario: Deadline absorbs a cold start under load

- **WHEN** the LLM CLI cold-starts while the machine is under concurrent load and takes substantially longer than a warm call
- **THEN** the deadline is large enough that the call completes rather than being terminated by the deadline

### Requirement: Retry once on a transient LLM failure

The system SHALL retry the LLM name request a single time, after a short
backoff, when the CLI exits non-zero — so a momentary overload, rate limit, or
budget blip does not permanently strand the original slug. The retry SHALL NOT
apply to the skip cases (CLI unavailable, empty prompt) or to output that ran
successfully but failed validation; those return immediately as before. If the
retry also fails, the system SHALL fail open with the (now diagnosable) error.

#### Scenario: First call fails, retry succeeds

- **WHEN** the first LLM CLI invocation exits non-zero and a retry returns a valid name
- **THEN** the task is renamed to the retried name

#### Scenario: Both attempts fail

- **WHEN** both the initial invocation and its single retry exit non-zero
- **THEN** auto-naming fails open, the slug is kept, and the logged error carries the CLI's failure reason

#### Scenario: Skip cases are not retried

- **WHEN** the CLI is unavailable or the prompt is empty
- **THEN** the request returns the skip result immediately without a retry

### Requirement: Budget cap sized above measured per-call cost

For backends whose CLI supports a per-call USD budget (currently Claude), the system SHALL cap each LLM name request with a per-call USD budget set
comfortably above the measured per-call cost, so a normal prompt — including a
moderately long pasted one — does not exceed the cap and fail. The cap SHALL be
documented against the measured token usage, not a stale estimate. Codex naming SHALL explicitly use the low-cost `gpt-6-luna` model with low reasoning effort, rather than inheriting the task model; it SHALL NOT pass Claude’s budget flag or fall back to a more expensive model when unavailable.

#### Scenario: Normal call stays within budget

- **WHEN** Claude auto-naming runs for a typical task prompt
- **THEN** the per-call budget passed to the CLI exceeds the measured per-call cost with headroom, so the call is not terminated for exceeding budget


### Requirement: Use the task's resolved backend for naming
The system SHALL generate automatic task names using the backend family selected for the task after backend routing and default resolution, with the task's resolved account authentication context where applicable. Backend-specific naming capabilities SHALL be declared through the backend family registry. Naming SHALL NOT silently invoke Claude or another backend when the selected backend cannot generate a name.

#### Scenario: Task uses a non-Claude backend
- **WHEN** a task is created using a non-Claude backend that supports non-interactive naming
- **THEN** its background name request uses that backend's naming invocation and does not invoke Claude

#### Scenario: Backend chosen by routing
- **WHEN** routing selects a backend different from the global default
- **THEN** auto-naming uses the final resolved task backend, not the global default

#### Scenario: Task has a resolved account
- **WHEN** the task is assigned a named account
- **THEN** the naming request uses that account's backend authentication context rather than inadvertently using another account

#### Scenario: Selected backend cannot generate a name
- **WHEN** the selected backend has no naming capability or its CLI is unavailable
- **THEN** naming is skipped, the original slug is retained, and a backend-specific skip reason is logged without invoking another backend

### Requirement: Backend-specific naming remains an isolated utility
Naming invocations SHALL be non-interactive, bounded by the existing shared deadline and retry policy, and configured to avoid tools, MCP connections, project context, and persistent task sessions wherever the backend supports those controls. Output SHALL pass the existing name validation before persistence. Claude naming SHALL retain its lightweight model and per-call budget cap; backend-only flags SHALL NOT be passed to unrelated CLIs.

#### Scenario: Backend-specific flags
- **WHEN** a naming request uses a non-Claude backend
- **THEN** the invocation uses that backend's supported utility flags rather than Claude-specific model, budget, or isolation flags

#### Scenario: Naming failure
- **WHEN** the selected backend fails, times out, or returns invalid output
- **THEN** the original slug is retained, existing rename race protections remain intact, and failure diagnostics identify the selected backend
