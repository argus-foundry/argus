## MODIFIED Requirements

### Requirement: Backend listing carries family metadata

`GET /api/backends` SHALL include, per configured backend, its family `kind`, `label`, prompt `skill_prefix`, and `session_switcher` metadata. It SHALL also include the reserved `dynamic` selector with `name` and `kind` equal to `dynamic`, `label` equal to `Dynamic`, and no command metadata. The `dynamic` selector SHALL NOT be creatable, updatable, or deletable as a command backend. Clients SHALL use these fields instead of hardcoding backend names.
