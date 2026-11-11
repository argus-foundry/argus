## Context

The resident uses the shared Hera page with a reader scoped to the standing orchestrator. Optional reader methods must be forwarded explicitly through that wrapper. The page currently prohibits the board toggle in resident mode. Attention polls currently reflect unread conversation replies alone.

## Goals / Non-goals

- Make the standing coordinator's board behave like the Projects board, including unread arrivals during an open tab, navigation, reply and human dismissal.
- Keep every existing resident rail restriction and session lifecycle rule.
- Preserve the existing unread-reply acknowledgement behavior; board items are read only through board interaction.
- Resident web/macOS parity remains the named `add-resident-web` follow-up; no REST changes are needed here.

## Decisions

- Forward the optional board listing method through `ResidentReader`; use the existing board model builder and mutation callbacks.
- Detect new unread resident board versions during model refresh so auto-show does not undo a deliberate toggle on every refresh. Preserve Projects' existing refresh-view behavior.
- Read board attention alongside the existing background resident poll. Store it separately from reply unread so viewing the conversation cannot acknowledge a reminder.
- Generate resident board help from the live board keymap and expose context-appropriate board actions in the palette.

## Risks / Trade-offs

- Board `r` overlaps rail rename: dispatch board keys only in the focused board, preserving worker management on the rail.
- Optional reads can fail: retain the last attention count on poll failure and log the error rather than silently clearing attention.
