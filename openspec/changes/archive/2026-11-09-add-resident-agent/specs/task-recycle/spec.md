## ADDED Requirements

### Requirement: Resident recycle seed re-reads the vault

When the recycled task is the resident, the seed prompt SHALL order: the caller-supplied handoff note first, then an instruction to read `resident/open-loops.md`, then `resident/profile.md`, then continue, then the resident's persistent orientation as background. The seed SHALL need no further tool calls beyond those reads. All other tasks SHALL keep the existing handoff-then-original-prompt seed unchanged.

#### Scenario: Handoff, then open loops

- **WHEN** the resident is recycled with a handoff note
- **THEN** the composed seed places the handoff note before the instruction to read `resident/open-loops.md`, which precedes the orientation

#### Scenario: Plain tasks unchanged

- **WHEN** a non-resident task is recycled
- **THEN** its seed is the existing handoff note followed by the original prompt
