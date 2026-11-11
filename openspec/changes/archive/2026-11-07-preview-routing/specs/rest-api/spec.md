## ADDED Requirements

### Requirement: Routing preview

`POST /api/routing/preview` SHALL accept `{"project","backend","archetype","profile"}` and return 200 `{"backend","kind","models","model_policy","default_model","routing_reason","advisory":true}` using the same routing and model resolution as task creation. `default_model` SHALL be null when the CLI chooses. The endpoint SHALL create no task, worktree, binding or usage reservation, SHALL NOT expose account identifiers, and SHALL emit no event. A missing project or malformed body SHALL be 400 `invalid_request`, an unknown project 404 `project_not_found`, no valid backend candidate 422 `backend_unavailable`, and an unexpected failure 500, each as `{"error","code"}`. Task creation SHALL revalidate an explicit model against the final routing result.

#### Scenario: Preview matches creation
- **WHEN** preview and creation run with identical inputs and configuration
- **THEN** both resolve the same backend

#### Scenario: No candidate
- **WHEN** the requested backend is not configured
- **THEN** the response is 422 with code `backend_unavailable`

#### Scenario: Routing changes after preview
- **WHEN** routing changes between preview and creation so the chosen model is no longer allowed
- **THEN** creation is rejected with `model_not_allowed`
