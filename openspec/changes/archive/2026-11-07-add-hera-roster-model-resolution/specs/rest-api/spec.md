## ADDED Requirements

### Requirement: Hera roster resolved model choices

Every role in `GET /api/hera` SHALL carry `archetype` and `model_resolution` with `requested_model`, `effective_model` (null when the CLI picks its own default or resolution is unavailable, never a fabricated ID), `effort`, `source` (`task_override`, `profile`, `backend_default`, `cli_default`, `unavailable`), `basis` (`current_config` for a bound role, `planned_intent` for an unbound one), `profile`, and `warning` (null or a safe message). Resolution SHALL use `agent.ResolveModel` precedence through the shared `internal/hera/tiering` package, SHALL NOT expose filesystem paths or credentials, and SHALL NOT claim a current-config result is an observed running model.

#### Scenario: Requested and effective differ
- **WHEN** a bound task has an invalid `model` override and a valid profile
- **THEN** `requested_model` is the override, `effective_model` is the profile's model, and `source` is `profile`

#### Scenario: Unbound planned role
- **WHEN** a role has no binding
- **THEN** `basis` is `planned_intent`

#### Scenario: Missing profile
- **WHEN** an explicitly bound profile is missing or invalid
- **THEN** `warning` is `profile "<name>" missing or invalid` and the roster still returns 200
