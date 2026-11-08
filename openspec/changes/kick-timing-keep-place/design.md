## Context

Both TUI surfaces (Tasks agent view `maybeKickRerender`, Hera panes `heraKickRerender`) funnel into `App.maybeKickRerenderAtWidth`, which calls `agent.ShouldKickRerender`. The API resize handler inlines the same gates. Busy/prompt deferrals already exist; Hera retries via `onDeferred` (10 s doubling to 60 s), the Tasks view only on re-entry.

The Claude Code mod API (engine types) exposes `$.prompt.read()` → `{text, cursor}`, `$.prompt.fill({text, mode, cursor})` → `{isFilled, refusal}`, and `prompt.change` events with an `origin` (`history|clear|undo|plugin|engine`). No screen scraping is needed for Claude.

## Decisions

### D1. Two new decisions in the one shared predicate

`ShouldKickRerender` gains `scrolledUp` and `hasDraft` inputs and `RerenderDeferScrolled` / `RerenderDeferDraft` outputs, checked after idle and before prompt. Order after margin: busy → prompt → draft → scrolled. Pure function, table-tested. The API handler mirrors the draft gate (the file comment already requires gates stay in sync).

### D2. Scroll gate is evaluated twice

- In the Hera pane's `maybeKickPaneRerender`, BEFORE calling the kicker: while the pane's `ScrollOffset() > 0` the pending dwell is held open (no RPC, no log spam); when it returns to 0 the dwell restarts with `KickDebounce`. This gives immediate re-arm on return to bottom, rather than waiting a 10–60 s retry.
- In `maybeKickRerenderAtWidth`'s `QueueUpdateDraw` callback via a `scrolled func() bool` (read fresh on the UI goroutine), covering the RPC round trip and the Tasks view. On `DeferScrolled` it does what DeferBusy does (invalidate attach cache, `recordUnreconciledCommittedCols`) so the drift anchor survives.

### D3. Tasks-view retry

The Tasks view has no retry path. Add `App.kickWait map[string]time.Time` (task → time it first looked clear). `onTick`, for the task currently shown in agent view, re-evaluates when the wait is set, the pane is at the bottom and the draft is clear, after a `KickDebounce` dwell. Cleared on entry, exit, session change and successful kick.

### D4. Draft signal (precedence)

1. Claude with a **live bridge** (`bridge.State.HasDraft`, from the mod). `State` JSON carries only the boolean; the text lives in a `json:"-"` field so REST/Tailscale never exposes it.
2. Otherwise a **TUI-local typed-since-submit flag** kept in `RemoteSession.WriteInput` for `OriginUser` input via a pure helper: CSI/SS3 sequences ignored (arrows, mouse, focus), printable bytes and bracketed-paste content set it, `\r`, Ctrl+C and Ctrl+U clear it, DEL does not unset it. Conservative: typed-then-erased defers until the next submit. Exposed through an optional `ComposerDirty() bool` interface (type-asserted; mocks unaffected).
3. `ScreenRenderer.InputDraft` is NOT used: it only recognizes Claude's composer marker, so it reports `found=false` for Codex/pi/opencode, which would silently mean "no draft".

Local-only limits: input typed in the web app or another TUI is invisible to (2). Accepted; the bridge path covers Claude everywhere.

### D5. Save-and-restore protocol (mod ⇄ daemon)

The mod lives in the process the kick kills, so the draft travels through the daemon continuously rather than at kill time:

- **Save.** `prompt.change` hook → debounced 300 ms (empties flush immediately, since a submit arrives as `origin: clear`) → `draft` event `{text, cursor}` over the existing events POST. Hub stores `Draft{Text, Cursor, At}` per task and exposes `HasDraft`.
- **Restore.** On `session.start`, if `bridge.keep_place`, the new mod POSTs `bridge/draft` `{generation, boot}`. The daemon returns the draft only if: the request's boot differs from the draft's boot (new process), the draft is non-empty, younger than 2 minutes, and not already taken; taking marks it consumed. The mod calls `$.prompt.read()`; only if the box is still empty does it `$.prompt.fill({text, mode:'replace', cursor})`, and reports `restored|skipped_nonempty|refused:<reason>` back for uxlog.
- **Fail open.** Any error leaves the session untouched. Old mod → no draft events → daemon holds none. Old daemon → pull 404s → mod does nothing.
- Attachments/image chips are not in `text`; a draft with an attachment restores as text only (documented limitation).

### D6. Off switch

`BridgeConfig.KeepPlace` (`bridge.keep_place`, default true), Settings → Bridge row, config.toml override like its siblings. Read live: gates D1–D3 in the TUI, the API handler gate, mod draft reporting (daemon rejects `draft` events when off) and the restore pull.

## Risks

- Mod change re-records the spawn-surface digest; old supervisors keep the old mod (handled by fail-open above).
- Restore can only be fully proven live (a real resumed Claude box); offline tests cover the daemon protocol, the mod hooks against fakes, and the gates. A live probe needs Aaron's go.
- Typed-since-submit false positives only delay a re-wrap.

## Non-Goals / named follow-ups

- Web/PWA and macOS scrolled-up gating (scroll state is client-side) and non-bridge draft gating for web resize kicks.
- Restoring attachments; restoring for non-Claude backends.
- No `screen.Sync()`; no supervisor `ProtocolVersion` change.
