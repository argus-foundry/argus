## Why

`PUT /api/projects/{name}` rebuilt the stored project from a request that had no `profile` field, so any REST edit silently reset the project's diligence profile. Task model choices could then change after an unrelated settings edit.

## What changes

- `GET /api/projects/full`, `POST` and `PUT` responses carry `profile` (`""` = default).
- `POST` accepts `profile`. `PUT` treats an omitted `profile` as "preserve", `""` as "clear" and a name as "set".
- An unknown nonempty profile returns 400, an unknown project on `PUT` 404, a project defined in config.toml 409.
- A committed change emits `config.changed` with `{"resource":"projects","name":...}` and no task id.

## Capabilities

### Modified capabilities

- `rest-api`: project CRUD profile field, errors and event.
- `diligence-profiles`: REST edits preserve the binding.

## Non-Goals

- No web, TUI or macOS change. Web follow-up: ENG-1074 (Edit projects without losing their diligence profile).

## Impact

- `internal/api/handlers.go`, `internal/config/file.go`, `internal/db/projects.go`, `internal/model/event.go`, README Reference.

Named follow-up: Remote TUI project-profile transport and spinner editing. Existing adapter gaps remain outside this change. macOS controls remain in the native-client milestone.
