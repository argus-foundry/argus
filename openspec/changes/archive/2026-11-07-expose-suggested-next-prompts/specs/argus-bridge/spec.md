## ADDED Requirements

### Requirement: Bridge reports the provider's suggested next prompt
The mod SHALL report `suggestion.changed {text}` only for a next-prompt suggestion the Claude engine itself proposed and the prompt box will show, never for a plugin-origin proposal, blank text, text over 4 KiB or text containing NUL, and SHALL NOT write the suggestion to the session or the prompt box. The daemon SHALL store one suggestion per task with session id, launch generation and receive time, SHALL ignore a suggestion carrying no or a non-current generation, and SHALL clear it on `suggestion.cleared`, a new turn, a session or generation change or a new mod boot. It SHALL reject a batch containing an invalid suggestion with 400 and keep the stored state.

#### Scenario: Engine suggestion stored
- **WHEN** the current launch reports `suggestion.changed` with text
- **THEN** the hub holds the text with the session id, generation and receive time and nothing is written to the PTY

#### Scenario: Cleared by a new turn
- **WHEN** a `turn.start` follows a stored suggestion
- **THEN** the stored suggestion is empty

#### Scenario: Oversized text
- **WHEN** a suggestion's text exceeds 4096 bytes
- **THEN** the response is 400 and the previous suggestion is unchanged
