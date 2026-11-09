## Decisions

- **Entry point.** `MoveTaskAccount` delegates to `MoveTaskBackend` when `Backend` is set, so REST, MCP, TUI and the remote client share one path. Same-family backends are rejected (use a plain account move).
- **Seed persistence.** The seed is persisted as `Prompt` (so a fresh restart from any surface still delivers the handoff); the first original prompt is kept in task_meta `backend_move/original_prompt` and used by later seeds, so repeated moves never nest seeds.
- **Row writes.** One targeted setter writes backend, model, account, routed, session id and prompt and returns the previous values for rollback. Model is cleared because model names are backend specific.
- **Session id.** Codex captures its ID after launch while it is empty (the old Claude ID is cleared so capture cannot skip). Claude targets get a minted pinned ID, like task creation.
- **Restart.** Live session: `Runner.Recycle` (resume=false) with the new backend/account on the wire (RecycleReq already carries both). No live session: persist only; callers use their normal restart path.
- **Capture race.** Codex capture takes the newest main thread for the cwd; a session that exits before its thread is written can capture an older thread for the same worktree. Accepted and documented.
