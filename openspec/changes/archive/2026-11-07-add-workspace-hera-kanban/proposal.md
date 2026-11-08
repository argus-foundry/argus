# Workspace Hera kanban

Coordinator trees need triage within the unified project rail. Group root coordinator trees by Active, Backlog, Blocked and Done, retaining plain tasks and nested trees. Use the shared row menu for kanban and orchestrator pin changes through existing authenticated endpoints.

## Abstraction inventory

- Coordinator tree owns its live coordinator binding, descendants and orchestrator triage state.
- Kanban group contains root coordinator trees within one project and orders pinned orchestrators first.
- Kanban disclosure remembers per-project group expansion using guarded workspace preferences.
- Hera kanban actions own optimistic overrides, cancellation, rollback and shared error feedback.

## Scope

Frontend only. Preserve task rows, status slots, indentation and ordinary task pin behavior. No Projects tab, Hera visibility toggle or Freelance section. Nested coordinators offer pinning but no kanban mutation because the existing endpoint accepts root orchestrators only. TUI and original SPA already expose Hera triage. Native parity remains tracked by native-workspace-rebuild and add-web-hera-mutations.
