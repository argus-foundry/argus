## ADDED Requirements

### Requirement: Managed browser MCP policy is process scoped

Sandboxed Claude and Codex task launches SHALL receive the shared managed browser server through backend-family launch integration. Known user/account, project and enabled-plugin local Playwright launchers SHALL be overridden or suppressed for that process, including duplicate namespaced aliases. Unrelated MCP servers and explicit attached-browser integrations SHALL remain available. Source configuration files SHALL remain unchanged. Unsupported or ambiguous conflicts SHALL produce actionable diagnostics and SHALL NOT silently fall through to unmanaged launching. These guarantees SHALL apply to fresh, resumed, recycled, supervisor-owned and in-process sessions.

#### Scenario: Plugin duplicates a project browser server

- **WHEN** a supported CLI loads both project and plugin local Playwright entries
- **THEN** only the managed launcher is usable for owned browser work and unrelated tools remain available

#### Scenario: Precedence cannot be established

- **WHEN** a malformed source or unsupported CLI prevents selective conflict suppression
- **THEN** browser automation is reported unavailable for that launch without altering source configuration or claiming the policy was enforced

#### Scenario: Account-specific resume

- **WHEN** a sandboxed task resumes using a selected Claude or Codex account
- **THEN** the same browser policy is applied using that account's effective configuration and the source files remain unchanged
