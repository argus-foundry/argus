## ADDED Requirements

### Requirement: Pi receives native Argus MCP registration
Argus SHALL register its current streamable HTTP MCP endpoint as the `argus` server in Pi's user-level native MCP configuration before launching Pi. Registration SHALL preserve unrelated servers and configuration fields. Invalid existing configuration SHALL NOT be overwritten and failures SHALL be logged.

#### Scenario: Fresh Pi configuration
- **WHEN** Argus launches Pi without an existing MCP configuration
- **THEN** Pi's user-level MCP configuration contains an enabled Argus server pointing to the configured Argus MCP endpoint

#### Scenario: Existing configuration
- **WHEN** Argus launches Pi with existing MCP settings and a changed Argus MCP port
- **THEN** the Argus registration uses the current endpoint and unrelated settings remain unchanged

#### Scenario: Malformed configuration
- **WHEN** Pi's existing MCP configuration cannot be parsed
- **THEN** Argus preserves the file and logs the injection failure
