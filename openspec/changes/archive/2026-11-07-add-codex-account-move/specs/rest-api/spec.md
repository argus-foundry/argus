## MODIFIED Requirements

### Requirement: Move a task to another account over REST

`POST /api/tasks/{id}/account` SHALL work for every backend whose `account_move.supported` is true, serialized with other moves and restarts of the task, and SHALL emit `task.account_changed` `{from,to,resumed,started}` once the account is persisted. A closed-out Hera worker with no live session SHALL get 409 `hera_closeout` unless `force_revive` is true. A restart failure SHALL return 500 `{"error":"restart failed","code":"restart_failed","account","moved","started":false,...}`: `moved:true` when the account stayed on the target, `moved:false` with `rolled_back:true` when the old session kept running and the account was restored.

### Requirement: Backend listing carries account-move support

`GET /api/backends` SHALL include `account_move: {supported, preserves_conversation}` per backend, true only for families with a tested conversation transfer.
