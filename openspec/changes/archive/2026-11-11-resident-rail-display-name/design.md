## Context

The resident and Projects tabs share one HeraPage and rail. The resident orchestrator and coordinator retain their fixed `resident` identity.

## Goals / Non-goals

- Display the configured name on the resident rail, following Settings and periodic config refreshes.
- Preserve every model identity, worker label, Projects label and jump-switcher label.
- No REST changes or additional frontend work: this is a local TUI rendering change.

## Decisions

Use a rail-only display override supplied by HeraPage's resident-title setter. Validate with `config.ValidResidentName`, the existing name rule, and use `resident` on failure. Clear the override when leaving resident mode. Update the setter from both existing App label-refresh paths so renames never rebind the pane.

Changing the model name would affect routing and other readers, so the override applies only while drawing the coordinator heading.

## Risks / Trade-offs

- A shared rail could leak the override to Projects → clear it on mode exit and cover mode switching in tests.
- A config refresh could leave the rail stale → cover Settings renames and both label refresh paths.
