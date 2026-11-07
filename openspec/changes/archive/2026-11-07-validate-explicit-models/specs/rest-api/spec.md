## ADDED Requirements

### Requirement: Explicit task model is validated before creation

When a create request carries a non-empty `model`, the server SHALL validate it against the resolved backend (after backend resolution) before any worktree, database row, or session is created. A backend with a configured `models` list SHALL accept exactly that list; a backend with a curated family list SHALL accept exactly that list; a backend with neither SHALL accept any identifier. A rejected model SHALL produce 422 with `{"error":"model is not allowed for resolved backend","code":"model_not_allowed","backend":<name>,"requested_model":<id>,"allowed_models":[...]}`. The same validation SHALL apply to JSON and multipart creation. An empty `model` SHALL always pass.

#### Scenario: Curated backend rejects unknown model
- **WHEN** a create request names a curated backend and a model outside its list
- **THEN** the server responds 422 with code `model_not_allowed` and no task or worktree exists

#### Scenario: Freeform backend accepts custom model
- **WHEN** a create request names a backend with no configured or curated list and any model
- **THEN** the task is created and records that model

### Requirement: Backend listing reports model policy

`GET /api/backends` SHALL include `model_policy` per backend: `configured` when the merged config defines `models`, `curated` when the family has a curated list, otherwise `freeform`. `models` and `model_policy` SHALL reflect the complete merged backend, including command overrides.

#### Scenario: Configured list reported
- **WHEN** `config.toml` defines `models` for a backend
- **THEN** its entry has `model_policy` `configured` and that list as `models`

### Requirement: Explicit schedule model is validated before saving

REST and MCP schedule creation and update SHALL validate a non-empty explicit model against the fresh merged backend selected by the same explicit, project, routing and default precedence used at task creation. Resolution at save time SHALL NOT reserve a launch. REST SHALL return the structured 422 model_not_allowed response used for task creation; MCP SHALL return a tool error identifying the rejected model and backend. A rejected save SHALL leave the stored schedule and run bookkeeping unchanged. Empty models SHALL pass. Validation at fire time SHALL remain in CreateAndStart because backend configuration and routing may change after saving. Existing LastRunAt and one-shot retry semantics SHALL remain unchanged.

#### Scenario: Invalid recurring or one-shot model is rejected on save

- **WHEN** a schedule is created or updated with a model outside its resolved backend policy
- **THEN** the save fails without inserting or changing a schedule

#### Scenario: Allowed model is saved

- **WHEN** a schedule carries a model permitted by its resolved backend
- **THEN** the model is saved and existing run bookkeeping is preserved
