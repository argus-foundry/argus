## Why

A coordinator board decision today is a one-line subject plus a free-text body. Aaron answers it with `r`, which pastes `Re: <subject> (#id)` into the coordinator terminal and leaves him to type the reply. That works for one question. It works badly for a round of eight: each answer is typed from scratch, the options live in prose the coordinator has to re-parse, and nothing records which option Aaron picked.

Aaron's personal `/decision` skill already solves the answering half. It builds a page with a question, a plain-language situation, two to four options (label, summary, upside, downside), a recommendation with a reason, a "something else" escape, and a note per decision. He used it for an 8-decision round and it worked very well. The skill's limits are that it publishes a claude.ai artifact and the answers come back by copy and paste, outside Argus.

This change makes that flow native. A coordinator posts a decision with structured options on its board. Aaron presses one key in the TUI, his browser opens a page served by the Argus daemon, he answers the round, and the coordinator receives one message summarizing every answer. The answers are also stored on the board items, so they survive recycles and show up in history.

## What Changes

- **Structured decision data on the board item.** A `decision` item gains an optional option set (two to four options with id, label, summary, upside, downside), an optional `recommended` option id, and an optional `reason`. The existing `body` doubles as the situation text. Plain free-text decisions keep working unchanged; nothing about `r`, `x`, Enter expansion or the accordion changes.
- **MCP.** `hera_board_post` and `hera_board_update` accept `options`, `recommended` and `reason`. An update that omits `options` keeps the stored structure; `options: []` clears it. Tool descriptions, the builtin `hera` skill and coordinator orientation tell coordinators to post structured decisions whenever a decision has distinct options.
- **TUI.** The board renders a structured decision readably (body, then the options with upside and downside, then the recommendation). A new rebindable board key, default `o`, opens the answer page for the focused coordinator's board. `r` is untouched and remains a plain reply. The keymap, help overlay, board hint row and README keybinding table are updated together.
- **Daemon-served decision page.** A new standalone page at `GET /decide`, served by the existing REST server, renders every open `decision` item on one coordinator's board as one round in the `/decision` layout. Structured items show option cards; plain items show a free-text card. Aaron picks an option or "something else", adds a note, and submits. Unanswered cards stay open.
- **Auth with no secret in shell history or logs.** The TUI asks the daemon for a single-use, 90-second launch ticket and opens `/decide?role=<id>#t=<ticket>`. The page exchanges the ticket for a short-lived session credential that can only read and answer that one coordinator's board. The master and device tokens never appear in a URL.
- **Durable answers and one coordinator message.** Submitting resolves each answered item with `resolved_by=human`, records the chosen option id and the note, and writes one self-addressed Hera message summarizing all answers in the same transaction. The existing board-notice outbox delivers it. Partial submits, "something else", and items the coordinator edited after the page loaded are handled per item.
- **Parity.** The web page is the answer surface and the TUI is the launcher. The macOS app is a named follow-up. The web SPA gets no board view in this change.

## Capabilities

### New Capabilities
- `board-decision-page`: the daemon-served answer page, its launch ticket and scoped session, the round contents, per-item submit outcomes, and the single summary message.

### Modified Capabilities
- `coordinator-board`: structured decision fields on board items, the answer-resolution record, MCP post/update parameters, TUI structured rendering and the `o` launcher key in the hint row, and coordinator guidance to post structured decisions.
- `rest-api`: the decision-link, decision-session, decisions-read and answers endpoints, the decision-session auth class and its route allowlist, and the new item fields in the board JSON.
- `hera-view`: the board-focused `o` key, its behavior when the API is off, and its absence in remote mode.
- `keybindings`: the new rebindable `hera_board.decide` action.
- `mobile-pwa`: the service worker must pass `/decide` through to the network and the page must stay out of the cached shell, with `SW_VERSION` bumped.

## Impact

- **Code:** `internal/db` (schema columns, `hera_board.go` validation, a batch answer function), `internal/mcp/hera_board.go` and its schema, `internal/api` (new handlers, a decision-session store, an auth-middleware branch, an embedded `decide.html`, routes), `internal/tui/hera/board.go` and `internal/tui/keymap`, `internal/hera/board.go` (message wording only), `internal/skills/builtin/hera`, the coordinator orientation text, `internal/api/static/sw.js`.
- **Wire contract:** additive fields on board item JSON (`structured`, `resolution_choice`); four new endpoints. No existing endpoint changes shape.
- **Schema:** two additive columns on `hera_board_items`. No data migration.
- **Frontends:** TUI (launcher), web (new standalone page), macOS (named follow-up).
- **Docs:** gotchas in `context/knowledge/gotchas/` (messaging, web-remote, keybindings), README MCP-tool, REST-endpoint and keybinding tables.
- **Non-goals:** see `design.md` (macOS UI, SPA board view, `/decision` skill integration, SSE push to the page).
