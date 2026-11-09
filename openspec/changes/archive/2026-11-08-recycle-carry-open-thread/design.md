## Decisions

- **One shared helper.** `internal/lastexchange` is pure (path in, `Exchange` out; no agent/config/db imports) so `internal/hera` and `internal/daemon` share it without an import cycle. The daemon-side `recycleExchangeSection` resolves the transcript (Claude-family only, exact session ID, account config dir, physical-path fallback) and callers pass the rendered section string into the builders.
- **Genuine human message:** excludes plugin-delivered prompts (`The argus-bridge plugin sent a message…`, `[argus-delivery:`), `<system-reminder>`/hook-only content, bare tool_results, empty text, meta/sidechain entries and interrupt markers. A human message surfaced mid-turn inside a tool_result ("The user sent a new message while you were working:") is unwrapped.
- **Reply** = all assistant text blocks after the human message (tool_use skipped), keeping the END at 4KB; human capped at 2KB.
- **Timing:** `task_recycle` previously built the seed before the idle wait; it now builds at restart time. Hera's builder already runs at restart.
- **Never block:** every read/parse error is logged with a `[recycle]` prefix and the section is omitted.
