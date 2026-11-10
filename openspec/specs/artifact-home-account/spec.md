# artifact-home-account Specification

## Purpose

Usage routing can run a task under an account other than the one its project's claude.ai artifacts belong to. This capability resolves each project's artifact home account. When an agent can't publish there with Claude Code's built-in artifact tool, the agent is told to publish through the Argus `artifact_publish` tool instead (see `artifact-publishing`), so artifacts always land on the right account.
## Requirements
### Requirement: Project artifact home account resolution

The system SHALL resolve a project's artifact home account from config. It SHALL use the project's `[project_artifact_accounts]` entry when that entry names a valid Claude account. It SHALL otherwise use the project's `[project_accounts]` entry when that entry names a valid Claude account. It SHALL otherwise resolve no home account. A valid Claude account is the `default` account or a configured account with a `claude_config_dir`. An entry naming an unknown account, or an account with no `claude_config_dir`, SHALL be skipped and logged, never treated as fatal. The global `default_account` SHALL NOT be used as a fallback.

#### Scenario: Explicit artifact entry wins

- **WHEN** a project has `[project_artifact_accounts]` set to `personal` and `[project_accounts]` set to `default`
- **THEN** the project's artifact home account is `personal`

#### Scenario: Falls back to the project account entry

- **WHEN** a project has no `[project_artifact_accounts]` entry and `[project_accounts]` is set to `personal`
- **THEN** the project's artifact home account is `personal`

#### Scenario: Unmapped project has no home account

- **WHEN** a project appears in neither `[project_artifact_accounts]` nor `[project_accounts]`
- **THEN** the project has no artifact home account, even when `default_account` is set

#### Scenario: Unknown account entry is skipped

- **WHEN** a project's `[project_artifact_accounts]` entry names an account that is not configured, and its `[project_accounts]` entry is `personal`
- **THEN** the unknown entry is logged and the project's artifact home account is `personal`

#### Scenario: Account without a Claude config dir is skipped

- **WHEN** a project's `[project_artifact_accounts]` entry names a configured account that has only a `codex_home`, and its `[project_accounts]` entry is `personal`
- **THEN** the entry is logged and the project's artifact home account is `personal`

### Requirement: Artifact account export at spawn

When building an agent's launch command, the system SHALL export `ARGUS_ARTIFACT_ACCOUNT` set to the task project's artifact home account if both of these hold: the project has an artifact home account, and either the task's backend family lacks Claude Code's built-in artifact tool or the task's effective account differs from the home account. Otherwise the system SHALL NOT export `ARGUS_ARTIFACT_ACCOUNT`, and SHALL never export it with an empty value. The value SHALL be recomputed every time the launch command is built, including resumes, restarts and account-move restarts.

#### Scenario: Claude task on its home account

- **WHEN** a Claude task in a project whose artifact home account is `personal` launches on the `personal` account
- **THEN** `ARGUS_ARTIFACT_ACCOUNT` is not present in the agent's environment

#### Scenario: Claude task routed to another account

- **WHEN** a Claude task in a project whose artifact home account is `personal` launches on the `default` account
- **THEN** the agent's environment contains `ARGUS_ARTIFACT_ACCOUNT=personal`

#### Scenario: Non-Claude backend in a mapped project

- **WHEN** a Codex task in a project whose artifact home account is `personal` launches
- **THEN** the agent's environment contains `ARGUS_ARTIFACT_ACCOUNT=personal`

#### Scenario: Unmapped project

- **WHEN** any task in a project with no artifact home account launches
- **THEN** `ARGUS_ARTIFACT_ACCOUNT` is not present in the agent's environment

#### Scenario: Account move back to the home account

- **WHEN** a Claude task in a project whose artifact home account is `personal` was running on `default` and is moved to `personal`
- **THEN** the restarted agent's environment does not contain `ARGUS_ARTIFACT_ACCOUNT`

### Requirement: Artifact publish routing guidance

The system SHALL inject builtin routing guidance into every agent whose backend family receives builtin routing content. The guidance SHALL be inert when `ARGUS_ARTIFACT_ACCOUNT` is unset. When the variable is set, the guidance SHALL instruct the agent to publish and update claude.ai artifacts by calling the Argus `artifact_publish` MCP tool with `account` set to the value of `ARGUS_ARTIFACT_ACCOUNT` (and the existing artifact's link when updating), instead of the built-in artifact tool. When the result reports the account as unavailable, the guidance SHALL instruct the agent to ask the user, with its question tool, to choose between: publishing to one of the reported available accounts, opening the page in the browser with `artifact_open`, revealing it in Finder with `artifact_open` and `reveal`, or parking it for later. Parking for later means giving the user the saved copy's absolute path (`artifact_path`) and the exact `argus artifact publish <artifact_path> --account <home>` command to publish it later, with `--update <link>` added when the original call was an update. The guidance SHALL instruct the agent never to fall back to the built-in artifact tool on its own initiative.

#### Scenario: Guidance names the artifact_publish tool

- **WHEN** the builtin routing content is rendered for an agent
- **THEN** it names the `artifact_publish` tool with the account from `ARGUS_ARTIFACT_ACCOUNT`, the update-by-link form, and an instruction not to fall back to the built-in artifact tool on its own

#### Scenario: Guidance describes the unavailable-account question

- **WHEN** the builtin routing content is rendered for an agent
- **THEN** it instructs the agent, on an unavailable result, to ask the user to choose between publishing to an available account, opening in the browser, revealing in Finder, and parking for later, where parking gives the user the saved copy's `artifact_path` and the exact `argus artifact publish` command for it (with `--update <link>` for an update)

#### Scenario: Guidance self-gates when unset

- **WHEN** the builtin routing content is rendered for an agent
- **THEN** it states that the section is to be ignored when `ARGUS_ARTIFACT_ACCOUNT` is unset

