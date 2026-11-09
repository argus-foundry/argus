## MODIFIED Requirements

### Requirement: Artifact publish routing guidance

The system SHALL inject builtin routing guidance into every agent whose backend family receives builtin routing content. The guidance SHALL be inert when `ARGUS_ARTIFACT_ACCOUNT` is unset. When the variable is set, the guidance SHALL instruct the agent to publish and update claude.ai artifacts by calling the Argus `artifact_publish` MCP tool with `account` set to the value of `ARGUS_ARTIFACT_ACCOUNT` (and the existing artifact's link when updating), instead of the built-in artifact tool. When the result reports the account as unavailable, the guidance SHALL instruct the agent to ask the user, with its question tool, to choose between: publishing to one of the reported available accounts, opening the page in the browser with `artifact_open`, revealing it in Finder with `artifact_open` and `reveal`, or parking it for later. Parking for later means telling the user the local artifact name and the `argus artifact publish` command to republish. The guidance SHALL instruct the agent never to fall back to the built-in artifact tool on its own initiative.

#### Scenario: Guidance names the artifact_publish tool

- **WHEN** the builtin routing content is rendered for an agent
- **THEN** it names the `artifact_publish` tool with the account from `ARGUS_ARTIFACT_ACCOUNT`, the update-by-link form, and an instruction not to fall back to the built-in artifact tool on its own

#### Scenario: Guidance describes the unavailable-account question

- **WHEN** the builtin routing content is rendered for an agent
- **THEN** it instructs the agent, on an unavailable result, to ask the user to choose between publishing to an available account, opening in the browser, revealing in Finder, and parking for later

#### Scenario: Guidance self-gates when unset

- **WHEN** the builtin routing content is rendered for an agent
- **THEN** it states that the section is to be ignored when `ARGUS_ARTIFACT_ACCOUNT` is unset
