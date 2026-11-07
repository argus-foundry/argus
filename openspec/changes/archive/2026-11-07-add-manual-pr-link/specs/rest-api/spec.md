## ADDED Requirements

### Requirement: Task PR link endpoints

`GET /api/tasks/{id}/pr` SHALL return `{"source","url","state","lifecycle","checked_at"}` from stored state without invoking GitHub (`source` `manual|automatic`; `checked_at` RFC3339 or null). `PUT` with `{"url"}` SHALL accept only `https://github.com/<owner>/<repo>/pull/<n>`, canonicalize it, store it and return 200 with state and lifecycle `unknown` until verified; otherwise 400 with the association unchanged. `DELETE` SHALL clear the association and return 200 with `source` `automatic`. A missing task SHALL be 404 and a store failure 500. All three require authentication.

#### Scenario: Invalid URL

- **WHEN** PUT supplies a non-github.com or non-pull URL
- **THEN** the response is 400 and the stored association is unchanged
