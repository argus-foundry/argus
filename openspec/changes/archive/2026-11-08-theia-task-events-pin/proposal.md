# Task mutations invalidate the rail through SSE. Replace full raw pin writes with targeted POST /pin.

## Scope

ENG-1085, ENG-1084. Frontend only, local commits.

## Non-Goals

No daemon, SPA or macOS changes. Named follow-ups are SPA adoption of theia-task-events-pin and macOS adoption of theia-task-events-pin; existing TUI behavior remains unchanged. No live daemon verification. Pin recovery consumes the boolean pinned field from the ENG-1126 task DTO on load, reconnect/resync and legacy fallback snapshots. Older daemons that omit pinned retain cached pins. No extra raw pin reads remain.

## Abstraction inventory

TaskEvents owns stream and snapshot fences. RailActions owns captured task pin requests. Task snapshots own rail state and selection.

## Corrective review decision

Lial approved keeping the visible 30 second task reconciliation fallback until the first task.updated event on each stream connection. Reconnect resets that observation. The corrective archive restates all modified base requirements and their retained scenarios. Task pin invalidations read the single-task DTO only when changed_fields includes pinned. Selection and menu opening issue no pin reads. Snapshot observation fences preserve newer local and SSE state.
