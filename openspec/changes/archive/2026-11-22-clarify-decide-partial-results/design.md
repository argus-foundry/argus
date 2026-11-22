## Context

The reported FYIs have successful comment receipts followed by agent resolution. New closed-item comments correctly reject; mixed batches already commit successful siblings. Review currently uses whole-draft validity as a proxy for note validity.

## Goals / non-goals

**Goals:** Explain closed-item rejection, avoid false missing-note messages, and verify mixed-result retry isolation.

**Non-Goals:** Accept new comments on closed items, relax receipt fingerprints, or change submission authorization. TUI and macOS have no standalone queue feedback to change; both launch the shared Decide page.

## Decisions

- Return the current closed item with `invalid` for closed feedback rejection, after receipt replay and existing validation guards. This uses the existing optional result item rather than a new outcome.
- Derive note errors independently of response/annotation validity. Preserve conflict notes and explicit re-answering.
- Keep immutable sent payloads and action IDs unchanged until a definitive per-item result.

## Risks / trade-offs

- Closed content is returned only within the already authorized owning board; wrong-role and missing results remain content-free.
- Changed-payload receipt reuse must remain invalid without a misleading closed-item diagnosis; replay remains first.
