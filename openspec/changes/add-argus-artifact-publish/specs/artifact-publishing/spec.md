## ADDED Requirements

### Requirement: Per-account artifact tokens

The system SHALL read an `[artifact_tokens]` config table mapping an account name to a secret source string. It SHALL resolve the source through the secrets resolver inside the daemon, at publish time. It SHALL reject a value that is not a secret reference. The resolved token SHALL NOT appear in any agent environment, MCP tool result, REST response, error message, or log line.

#### Scenario: Token resolved at publish time

- **WHEN** `artifact_publish` is called for account `personal` and `[artifact_tokens]` maps `personal` to an `op://` reference
- **THEN** the daemon resolves that reference and sends the token as a bearer credential to the artifacts endpoint

#### Scenario: No token configured

- **WHEN** `artifact_publish` is called for an account with no `[artifact_tokens]` entry
- **THEN** the call fails with an error naming the account and the `[artifact_tokens]` table, and no network request is made

#### Scenario: Token never leaks

- **WHEN** any publish succeeds, fails, or finds the account unavailable
- **THEN** the token string does not appear in the tool result, the REST response body, the error text, or the log output

### Requirement: Publish a single HTML page

The system SHALL publish a single HTML file to the claude.ai artifacts endpoint (`https://api.anthropic.com/api/artifacts/mcp`) as the given account, and return the artifact's claude.ai link. Given an existing artifact link, it SHALL instead publish a new version on top of that artifact's current version. It SHALL reject, before any network call, a file that does not have an `.html` or `.htm` extension, does not begin with an HTML doctype or `<html`, or exceeds 16 MiB. An HTTP 401 from the endpoint SHALL produce an error telling the user to re-run `claude setup-token` for that account.

#### Scenario: New artifact

- **WHEN** `artifact_publish` is called with an HTML file, a title, and a configured account
- **THEN** a new private artifact is created on that account and the result contains its claude.ai link

#### Scenario: Update existing artifact

- **WHEN** `artifact_publish` is called with an HTML file and the link of an existing artifact on that account
- **THEN** the system looks up the artifact's current version and publishes the file as a new version based on it, returning the same link

#### Scenario: Invalid input rejected locally

- **WHEN** `artifact_publish` is called with a non-HTML file or a file larger than 16 MiB
- **THEN** the call fails with a descriptive error and no network request is made

#### Scenario: Expired token

- **WHEN** the artifacts endpoint answers HTTP 401
- **THEN** the error tells the user to re-run `claude setup-token` for that account and update its secret

### Requirement: Unavailable account handling

The system SHALL treat an HTTP 404 response to a tool call whose body says the artifact tools "aren't available to this session" as the account being unavailable. When the account is unavailable and a calling task is known, the system SHALL register the HTML file as an artifact of that task. It SHALL probe every other account in `[artifact_tokens]` with a real tool call, and SHALL return a non-error result containing `status: "unavailable"`, the requested account, the registered artifact filename, and the accounts whose probe succeeded.

#### Scenario: Exhausted home account

- **WHEN** `artifact_publish` is called for account `default` from a task and the endpoint refuses with the "aren't available to this session" 404
- **THEN** the HTML file is registered as an artifact of that task, and the result has `status: "unavailable"`, account `default`, the artifact filename, and the list of other accounts that can publish

#### Scenario: Other account also unavailable

- **WHEN** the home account is unavailable and the probe of another configured account is also refused
- **THEN** that other account is not listed as available

#### Scenario: No calling task

- **WHEN** the account is unavailable and no calling task can be resolved
- **THEN** nothing is registered and the result still reports the unavailable status and the available accounts

### Requirement: Open or reveal a registered artifact

The system SHALL provide an `artifact_open` MCP tool that opens a registered artifact of the calling task in the default application, or reveals it in Finder when `reveal` is true. It SHALL refuse any filename that is not in the calling task's artifact manifest or that fails the artifact path-escape defense. On a platform other than macOS it SHALL return a not-supported error.

#### Scenario: Open in default app

- **WHEN** `artifact_open` is called with a filename registered to the calling task and `reveal` false
- **THEN** the daemon opens that file with the system's default application

#### Scenario: Reveal in Finder

- **WHEN** `artifact_open` is called with a registered filename and `reveal` true
- **THEN** the daemon reveals that file in Finder

#### Scenario: Unregistered file refused

- **WHEN** `artifact_open` is called with a filename that is not registered to the calling task
- **THEN** the call fails and nothing is opened

### Requirement: Publish from the command line and REST

The system SHALL expose `POST /api/artifacts/publish` (authenticated like the other REST endpoints) and a CLI subcommand `argus artifact publish <file> --account <name> [--title <t>] [--update <link>]` that publishes through it. On success the CLI SHALL print the artifact link and exit 0. When the account is unavailable it SHALL exit with code 3 and print the accounts that can publish. On any other failure it SHALL exit non-zero with the error.

#### Scenario: CLI success

- **WHEN** a user runs `argus artifact publish page.html --account personal` and the publish succeeds
- **THEN** the artifact link is printed and the exit code is 0

#### Scenario: CLI unavailable account

- **WHEN** a user runs `argus artifact publish page.html --account default` and the `default` account is unavailable
- **THEN** the command exits with code 3 and prints which configured accounts can publish
