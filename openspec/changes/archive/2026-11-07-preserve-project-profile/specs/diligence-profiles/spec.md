## ADDED Requirements

### Requirement: REST project edits do not reset the profile binding

Editing a project's path, branch, backend or sandbox through REST SHALL NOT change its bound diligence profile unless the request sets `profile`.

#### Scenario: Model resolution is stable across a settings edit

- **WHEN** a project's path is edited via REST
- **THEN** the project's profile name is unchanged
