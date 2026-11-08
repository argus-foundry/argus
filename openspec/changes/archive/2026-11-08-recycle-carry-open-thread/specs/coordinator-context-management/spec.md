## ADDED Requirements

### Requirement: Handoffs carry an "Open with the user" section
Every instruction that tells a session to write a handoff or request a recycle (the context-budget nudge, the `hera_status` `handoff_note` description, the bounce instruction, the coordinator spawn orientation, and the hera skill) SHALL require an "Open with the user" section containing the user's last unresolved question or request, the session's latest answer or thinking on it, and any decision the user still owes, or "none".

#### Scenario: Budget nudge names the section
- **WHEN** the coord-hook emits the context-budget nudge
- **THEN** its text requires the handoff_note to include an "Open with the user" section

### Requirement: Recycled hera seed relays what is open with the user
The hera recycle seed prompt SHALL include a "Last exchange with the user before the reset" section extracted by the daemon from the prior Claude session transcript (last genuine human message and the session's reply after it), and SHALL direct the fresh session to relay a non-empty "Open with the user" section or that exchange as its first user-visible reply, without announcing the recycle mechanics.

#### Scenario: Forced recycle still carries the exchange
- **WHEN** a coordinator is force-recycled with no handoff_note
- **THEN** the seed prompt contains the last human message and the prior reply from the transcript

#### Scenario: Extraction failure never blocks the recycle
- **WHEN** the transcript is missing, unreadable, or the backend is not Claude
- **THEN** the section is omitted, a `[recycle]` log line is written, and the recycle proceeds

#### Scenario: Non-genuine messages are ignored
- **WHEN** the latest user-role entries are plugin-delivered prompts, system-reminder-only content, tool results, or empty
- **THEN** the extracted human message is the latest genuine human message before them, and a human message surfaced mid-turn inside a tool result is included with its wrapper removed
