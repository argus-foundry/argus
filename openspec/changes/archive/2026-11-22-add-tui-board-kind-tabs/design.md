## Context

The board owns item selection and all board commands; the separate inbox modal displays role messages.

## Goals / Non-goals

Show Decisions, FYIs, Actions, Todos, All with open counts and bounded rendering.

Non-goals: changing the message inbox or web Decide page. Named follow-up: macOS Hera board kind tabs, alongside its existing read-only Hera parity gap.

## Decisions

Keep the complete open-item snapshot separately from the filtered item list so every existing command uses the visible selection. Remember tab indices in the Hera page keyed by orchestrator. Reserve one row for tabs after reserving the footer. Use theme badge tokens matching the web dark palette. Plain arrows select tabs; modified arrows retain pane traversal.

## Risks / Trade-offs

- Narrow panels cannot show full labels: abbreviate, then clip a window starting at the selected tab.
- Filtered snapshots must not hide unread arrivals: detect arrivals and title attention using the complete snapshot.
