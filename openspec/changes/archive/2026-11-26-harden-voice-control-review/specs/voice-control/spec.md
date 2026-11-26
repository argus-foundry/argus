## ADDED Requirements

### Requirement: Voice mutations require the API master token

The MCP server SHALL require the Argus API master token in an Authorization bearer header for voice start, stop, resume, and send. These commands SHALL require a loopback HTTP host and, if present, a matching HTTP origin. A refused call SHALL NOT create or change a job or task. The credential SHALL NOT be accepted as a tool argument or query parameter.

#### Scenario: A voice client sends input without a credential

- **WHEN** a voice client calls send without the API master token in the bearer header
- **THEN** the call is refused and no session input is written
