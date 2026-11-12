## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Budget cap sized above measured per-call cost
For backends whose CLI supports a per-call USD budget (currently Claude), the system SHALL cap each LLM name request with a per-call USD budget set comfortably above the measured per-call cost. Codex naming SHALL explicitly use the low-cost `gpt-6-luna` model rather than inheriting the task model; it SHALL NOT pass Claude's budget flag or fall back to a more expensive model when unavailable.

#### Scenario: Claude budget cap
- **WHEN** Claude auto-naming runs for a typical task prompt
- **THEN** its per-call budget exceeds the measured cost with headroom

#### Scenario: Codex low-cost naming
- **WHEN** a Codex task runs on an expensive model
- **THEN** naming still requests `gpt-6-luna` with low reasoning effort
