## Context

Today's kick, from the code:

- **Predicate** (`internal/agent/rerender.go`): `ShouldKickRerender` / `ShouldKickRerenderKeepPlace` fire when `|panelCols - initCols| >= RerenderMargin (15)` or the same against the caller-tracked `committedCols` anchor (BUG-078/081), gated on idle, not blocked on a prompt, not scrolled up, no unsent draft.
- **Classic agent view**: `App.maybeKickRerender` (`internal/tui/app.go:5777`) on agent-view entry (`app.go:5736`) and via the `kickWait` retry in onTick (`app.go:5900-5909`). The decision core is `maybeKickRerenderGated` (`app.go:5980`); the stop is `sess.StopScoped(agent.StopRerender)` and the restart happens in `handleSessionExitUI` via `pendingRerenderRestart` (`app.go:6140-6145`).
- **Hera panes**: `HeraPage.maybeKickPaneRerender` (`internal/tui/hera/panes.go:369`) debounces with `KickDebounce` (300ms), `ZoomKickDebounce` (100ms), retry backoff, keep-place hold, and calls `App.heraKickRerender` (`app.go:5924`) which funnels into the same `maybeKickRerenderGated`.
- **Kick primitive**: `Runner.KickRerender` (`internal/agent/runner.go:341`) / supervisor `KickReq` (`internal/daemon/sessioncore.go:388`). Also used by `hera_revive`, account moves and closeout. **Unchanged.**
- **Existing border indicator**: `TerminalPane.Draw` already paints a right-aligned status on the top border (`internal/tui/terminal/terminalpane.go:1996-2002`, text from `RerenderStatus()`, `internal/tui/terminal/rerender.go:25-35`: "Re-render pending" / "Re-rendering…"). The title (`borderTitle`, "Agent"/"Coordinator") and `borderHint` sit on the left (`terminalpane.go:1973-1991`). The new notice reuses that slot.
- **Other clients**: the web SPA's `postResize` (`internal/api/static/index.html:3490`) sends `scrolled_up`/`composer_dirty` (`composerTypedFold`, `index.html:3439`) to `POST /api/tasks/{id}/resize`; the daemon decides (`handleResize` → `evalKickRerender`, `internal/api/handlers.go:1302-1390`) and returns `rerendered`/`deferred`. macOS (`macos/Sources/Argus/TerminalController.swift:291-298`, `macos/Sources/ArgusKit/KeepPlace.swift`) debounces and POSTs the same `/resize` with the same keep-place flags. So **for web and macOS the kick is decided daemon-side; for the TUI it is decided client-side** (TUI resizes through its own session handle, not `/resize`).

Spike facts (2026-11-08): Claude Code repaints its live region correctly on SIGWINCH in both directions with no scrollback writes; live confirmation was Aaron's Ctrl+Z-twice clearing a garbled pane with no kick firing.

## Goals / Non-Goals

**Goals:**

- A width change in the TUI never restarts the agent by default.
- The user can still re-render history on demand with one chord, and always knows when history is owed a re-render.
- One setting fully restores today's automatic behaviour.
- A restarted session never spawns at a stale size now that the auto-kick no longer papers over it.

**Non-Goals:**

- No paint-time soft-wrap of old history (rejected: mangles box tables and frames; display rows != emulator rows).
- No removal of the auto-kick, its debounce/backoff/keep-place plumbing, or `ShouldKickRerender*` (named follow-up once the no-restart default proves out).
- No change to `Runner.KickRerender`, `KickReq`, `hera_revive`, account move, or closeout.
- **Web SPA and macOS (named follow-up `resize-without-restart-clients`):** both keep today's daemon-side auto-kick on `/resize`; `ui.auto_rerender_on_resize` governs the TUI only in this change. The follow-up would make `handleResize` honour the setting, add `POST /api/tasks/{id}/rerender` plus an `owed` flag on the session status, and add the notice and a button/shortcut to web and macOS. Rationale: without an on-demand endpoint, switching the daemon path off would leave web/macOS users with no way to re-render at all.
- Hera tab macOS remains read-only (standing gap, unchanged).

## Decisions

### D1. Gate, don't delete

Add `config.UIConfig.AutoRerenderOnResize bool` (`toml:"auto_rerender_on_resize"`, default false), read live like `RerenderKeepPlace` (`internal/config/config.go:457`). Both automatic entry points consult one helper `App.autoRerenderOn()`:

- `maybeKickRerender` entry-time and `kickWait` retry callers (classic view);
- `HeraPage.maybeKickPaneRerender`'s evaluation (wire a `SetAutoRerender(func() bool)` mirroring `SetKeepPlace`, `panes.go:433`).

With it **on**, behaviour is byte-identical to today (all existing requirements apply). With it **off**, those paths skip the kick but still compute drift for the notice. The hotkey path calls the same `maybeKickRerenderGated` core with a `force` flag that bypasses the debounce and the `lastAttachCols`/margin short-circuits but **not** the safety gates (idle, not blocked on a prompt, keep-place scroll/draft). Alternative considered: delete the auto path now. Rejected per Aaron's rule that the switch must fully restore old behaviour, and to keep this PR reviewable.

### D2. Owed-re-render notice reuses the existing border slot

`TerminalPane` gains `SetRerenderOwed(on bool, hint string)`. `Draw` paints, in the same right-aligned top-border slot as `RerenderStatus`, in precedence order: "Re-rendering…" > "Re-render pending" > owed notice. The owed text is `width changed · <key> re-render` where `<key>` is `keymap.BindingFor(CtxAgent, ActAgentRerender)` resolved live, so a user rebind or an unbound action is reflected (unbound → `width changed · re-render via ctrl+k`, pointing at the palette). It is painted after terminal content like the existing indicator (deferred draw) so a prompt on the first content row stays readable, and is truncated to the border width, dropping the notice before it can overlap the left title or `borderHint`.

Who sets it: a per-tick evaluation (classic view in the existing tick path; Hera in `HeraPage` Draw after `SetRect`, where fresh pane width is known, cf. `panes.go:369`) calls the pure predicate `agent.MarginExceedsRerenderThreshold(initCols, paneCols) || MarginExceedsRerenderThreshold(committedCols, paneCols)` (the existing drift predicate, no new thresholds) and drives `SetRerenderOwed`. Cheap: it reads `InitialPTYSize` already cached by the handle plus the in-memory `committedCols`, no RPC. It clears when the width returns inside the margin, after a successful re-render (new process, anchors reset at `app.go:5986-5996`), or on rebind/unbind (`ResetVT`). When the setting is on, the notice is not shown (the kick handles it; the existing "Re-render pending/Re-rendering…" statuses cover the in-flight states).

Alternative: show it in the status bar. Rejected by Aaron's decision (it must be on the pane frame, mirroring the title).

### D3. Hotkey: `agent.rerender`, default `ctrl+n`; also clickable and in the palette

New action `ActAgentRerender` in `CtxAgent` (`internal/tui/keymap/actions.go`, `defaultSpecs` + `actionLabels` + `contextOrder`). It must be a chord argus intercepts because agent panes forward keys to the PTY, and `bindingAllowed` already forces a modifier in `CtxAgent` (`keymap.go:238`).

**Requirement from Aaron: works for anyone with no terminal setup.** That rules out Cmd chords and ctrl+shift+letter: macOS terminals consume Cmd+letter as menu shortcuts, and without the kitty keyboard protocol ctrl+shift+z arrives as plain ctrl+z (the zoom toggle, which argus also deliberately guards). `shift+home` is `fn+shift+left` on a MacBook and some terminals scroll their own buffer on Home/End. Only a plain `ctrl+<letter>` is delivered identically everywhere, one-handed.

**Audit.** Argus already uses in the agent context/global: ctrl+l, r, t, p, z, y, d, f, o, k, g, b, j (`actions.go:140-190`); ctrl+c/ctrl+q are structural. Claude Code's composer owns ctrl+a/e (line start/end), u/w/k (kill), o (transcript), b (background), plus c, d, g, h, m, j, s (stash), v (image paste), x (chord prefix: ctrl+x ctrl+k/e), r, t, y, z, l. `ctrl+n` is unused by argus. Claude uses it only for history-next / menu-down, which the Down arrow also does, so it is the cheapest chord to take. The loss is documented in the keybindings gotcha. Because the action is rebindable, a user who relies on ctrl+n in Claude can rebind it (the palette and click paths still work).

**Three universal paths, no setup:** (1) `ctrl+n`; (2) **the owed notice is clickable**, and a left click on it runs the re-render (a `MouseHandler` branch on the top-border cell range, gated on the notice being visible; must keep the existing click-passthrough/focus rules, see `add-click-passthrough` and the `TaskPage.MouseHandler` guard); (3) the command palette row (ctrl+k, "re-render at this width"). The notice text shows the live key, e.g. `width changed · ctrl+n re-render`.

Dispatch: add a `keymap.Resolve` case in `handleAgentKey` (classic) and in the Hera pane key handler, calling `App.rerenderNow(taskID, paneCols)`. Help overlay is generated (`modal.SectionsFromKeymap`), so the row appears from `contextOrder`; add the assertion in `help_test.go` (`TestHelpModal_Draw`) and a row in the README Reference keybinding table. **Command palette reach:** the palette's per-context registries are built from keymap metadata, so the row appears for CtxAgent focus (classic) and for Hera pane focus (extend the Hera pane literal/registry set if Hera panes do not route CtxAgent actions today; verify against `command-palette` requirement "Keymap metadata is the primary data source" and add a palette test).

Behaviour: no owed re-render (width inside margin) -> no-op with a status-bar notice "History already matches this width" (the chord never silently does nothing). Otherwise it runs the kick; if the agent is busy/blocked/scrolled/has a draft, the keep-place and prompt gates still defer it and a status-bar notice says why, setting the existing "Re-render pending" status and re-arming through the existing retry (`kickWait`/`onDeferred`). A forced re-render never kills a prompt-blocked agent.

### D4. Spawn size for recycled/kick-restarted sessions

`restartSessionWithSeedPrompt` (`internal/daemon/recycle.go:106-111`) copies `sess.PTYSize()`, and falls back to a hard-coded 24x80 when no session exists (`recycle.go:125-129`). `PTYSize()` (`internal/agent/session.go:724`) is the *current* PTY size, last set by `Resize`, so it is only wrong when no viewer ever resized the PTY (a pane that was never drawn, or detached since a layout change). Without the auto-kick nothing repairs that. New resolution order via one helper `resolveSpawnSize(database, role, sess)`:

1. fresh viewer report for the role: `db.InitialPTYSize(role, now)` (`internal/db/initial_pty.go:42`, TTL-bounded, role-aware; the same source `agent.CreateAndStart` uses at `internal/agent/create.go:381`);
2. live `sess.PTYSize()` if non-zero;
3. 24x80.

`taskRecycleRunner` (plain task) passes the zero role kind (Active layout). Wire impact: **none**. `RecycleReq`/`KickReq` already carry `Rows`/`Cols` (`sessioncore.go:388,439`) and the resolution happens in the daemon before the RPC, so `ProtocolVersion` (11, `internal/daemon/types.go:145`) is unchanged. The change is daemon-side only; confirm `internal/daemon/recycle.go` is not in the supervisor spawn-surface digest set (if it is, re-record per `gotchas/ci-gates.md`). The audit also covers `HeraReviveRunner.KickRerender` (`internal/daemon/revive.go:76`) and the account-move kick for the same stale-size inheritance.

### D5. Logging

`[rerender] owed notice shown task=%s pane=%s cols=%d init=%d committed=%d`, `... cleared task=%s reason=<rerendered|width-returned|rebind|setting-on>` (transition-only, never per frame); `[rerender] hotkey task=%s cols=%d outcome=<kicked|noop-no-drift|deferred-busy|deferred-prompt|deferred-scrolled|deferred-draft|error>`; `[rerender] auto_rerender_on_resize=%t` once at TUI start and on every live change; `[spawn-size] recycle task=%s source=<report|pty|default> rows=%d cols=%d`.

### D6. Frontend parity

| Surface | Today | This change |
|---|---|---|
| TUI (classic + Hera) | client-side auto-kick | no-restart default, notice, hotkey, setting |
| Web SPA | daemon-side kick via `/resize` (`handleResize`), keep-place flags | unchanged; named follow-up |
| macOS | daemon-side kick via `/resize`, `KeepPlace.swift` | unchanged; named follow-up |

The REST surface is not touched, so the parity trigger (new/changed endpoint/field) is not tripped; the follow-up is explicit in Non-Goals.

## Risks / Trade-offs

- **Duplicate rows in scrollback (about 13 near each resize) and cropped old history when narrower** -> accepted limitation, named in the notice semantics; hotkey repairs on demand.
- **`ctrl+n` taken from Claude's composer (history-next / menu-down)** -> the arrow keys do the same; rebindable; click and palette paths unaffected.
- **Mixed-client session**: a web/macOS viewer on the same task still triggers the daemon auto-kick at its own width, restarting the TUI's view too -> same as today; resolved by the client follow-up.
- **Two code paths (auto on / off) in `maybeKickRerenderGated`** -> mitigated by gating at the entry points only, leaving the core shared and exercised by both the setting and the hotkey; tests assert on-state is behaviour-identical.
- **Spawn-size report TTL expiry** -> falls back to live PTY size, then 24x80, as before.

## Open Questions

- Confirm `ctrl+n` as the default.
- Notice wording: `width changed · ctrl+n re-render`.
- Should the hotkey on a pane with no drift be a no-op (as specced) or force a re-render anyway (useful to repair other garbling)?
