# Model and sandbox choices during workspace task creation

The New task form needs the existing daemon creation options so tasks can start with the intended model and sandbox setting.

Add a model input with backend model suggestions and a backend default placeholder. Reset it when the backend changes. Add Project default, Enabled and Disabled sandbox choices. Keep both fields in each project draft and omit inherited defaults from POST /api/tasks.

## Scope
Only the optional web workspace changes. The TUI and mobile PWA already support these creation fields. macOS creation parity remains the named TUI-only creation follow-up in AGENTS.md. Task settings keep sandbox read-only.

## Abstraction inventory
| Name | Type | Description | Depends on |
| --- | --- | --- | --- |
| Creation model | Value | Optional model identifier and suggestions | Selected or resolved default backend |
| Creation sandbox | Value | Project default, enabled or disabled | Existing daemon creation contract |
| Project creation draft | Entity | Retains fields until successful creation or disconnect | Creation model and sandbox |

The existing NewTaskView owns the creation lifecycle. Model metadata and request normalization belong to a small creation policy alongside its account policy.
