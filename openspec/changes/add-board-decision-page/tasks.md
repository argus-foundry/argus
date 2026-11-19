## 1. Storage and validation (`internal/db`)

- [ ] 1.1 Add `structured TEXT NOT NULL DEFAULT ''` and `resolution_choice TEXT NOT NULL DEFAULT ''` to `hera_board_items` using the schema's existing additive-column mechanism; no data migration code
- [ ] 1.2 Extend `HeraBoardItem`, `boardColumns` and `scanBoard` with `Structured` (nil for plain) and `ResolutionChoice`; JSON tags `structured` and `resolution_choice`
- [ ] 1.3 Add structured validation beside `validBoardContent`: decision-only, 2-4 options, id regex and uniqueness and never `other`, label/summary/upside/downside/reason length caps, `recommended` names an option, 16 KiB serialized cap, rejected on resident-approval items
- [ ] 1.4 Thread structured data through `PostHeraBoardItem` and `UpdateHeraBoardItem`; a change is a material edit (unread + `updated_at` bump), an identical resubmission is not; update with absent options keeps the stored structure, empty options clears it
- [ ] 1.5 Add `AnswerHeraBoardItems(roleID, answers)`: one transaction under `d.mu`, per-item outcome (`resolved`/`conflict`/`already_resolved`/`not_found`/`invalid`), at most 16 answers and 4 KiB notes, resident-approval items invalid, resolved items set `resolved_by=human`, `resolution_choice`, `resolution_note`
- [ ] 1.6 In the same transaction insert exactly one self-addressed `hera_messages` row (`delivery_mode=pending`, tldr "Aaron answered N board decisions") listing resolved answers by id, subject, chosen label (mark the recommendation, or "Something else"), note, plus the still-open ids; no row when nothing resolved; no human-dismissal "dropped" notice on this path
- [ ] 1.7 Emit `hera.board_changed` once per resolved item after commit; `uxlog` `[board]` lines for each outcome (counts and ids only, never notes)
- [ ] 1.8 Tests (`internal/db`, in-memory DB, `t.Run` tables): validation matrix; post/list round trip; body-only update keeps options; `options: []` clears; structure change marks unread and bumps `updated_at`; identical resubmission does not; answer outcomes including conflict, already resolved, wrong role, plain item with `other`, `other` without note, unknown option; atomicity (items and message commit together; forced message failure rolls back items); exactly one message per batch and none when nothing resolved; no dropped-notice duplicate; message fits the 64 KiB cap at the 16-item/4 KiB limits

## 2. MCP tools (`internal/mcp/hera_board.go`)

- [ ] 2.1 Add `options` (array of option objects), `recommended` and `reason` to `hera_board_post` and `hera_board_update` schemas and decode structs; decode `options` so absent differs from empty
- [ ] 2.2 Update the tool descriptions: post structured decisions when there are distinct options, body = situation only, always recommend with a reason, press-`o` and answer-message semantics (see 8.1)
- [ ] 2.3 Extend the `boardStore` interface and server wiring; keep `hera_board_list` returning structured data and `resolution_choice`
- [ ] 2.4 Tests: schema-vs-decode-struct drift test (as for the `hera_plan` tools); post/update/list over MCP with structured data; omitted vs empty options; worker/other-coordinator rejection unchanged; invalid structures return tool errors

## 3. Decision tickets, sessions and auth (`internal/api`)

- [ ] 3.1 Add an in-memory decision store: tickets (hash, role id, 90 s TTL, single use) and sessions (hash, role id, 8 h TTL), constant-time compare, bounded size with expiry sweep, cleared on restart
- [ ] 3.2 `POST /api/hera/roles/{role_id}/board/decision-link` (master/device only; coordinator roles only; optional `focus_item_id` validated against the role's open decisions)
- [ ] 3.3 `POST /api/hera/board/decision-session` served without bearer auth (add the exact path to the middleware skip list); single-use exchange; 401 on unknown, expired or used tickets
- [ ] 3.4 Auth middleware branch: a bearer matching a decision session tags `X-Argus-Auth: decision:<role_id>` and is allowed only on the decisions and answers routes for its own role id, 403 everywhere else; confirm `requireMaster`, scope-token gates (MCP tool register, plugin views) and every other handler reject the tag
- [ ] 3.5 `GET /api/hera/roles/{role_id}/board/decisions` (open `decision` items, id order, no read-state side effects)
- [ ] 3.6 `POST /api/hera/roles/{role_id}/board/answers` (single JSON value consumed through EOF, 16-answer and note caps, `expected_updated_at` required, per-item results, `conflict` returns the current item)
- [ ] 3.7 Register routes; `uxlog` `[decide]` lines for mint, exchange, rejects, reads, outcomes; verify no log line, error string or DB row contains a ticket or session
- [ ] 3.8 Tests (`httptest`, no live daemon, `t.TempDir()` home): ticket single use and 90 s expiry (injected clock); session 8 h expiry; role scoping; allowlist 403 on every other route including MCP register and mint-link; master and device tokens on decisions/answers; mixed-outcome answers; nothing-resolved writes no message; trailing JSON, oversize and zero revision rejected; board JSON carries `structured` and `resolution_choice`; restart invalidation; log capture asserts no secret appears

## 4. The page (`internal/api/static/decide.html`, `handleDecidePage`)

- [ ] 4.1 Adapt `~/.claude/skills/decision/template.html` into an embedded `decide.html`: tally bar, option cards with upside/downside, Recommended pill and reason, dashed Something else card, note field, take-the-recommendation button, Copy answers fallback, system font stack and no external requests
- [ ] 4.2 Replace artifact-database persistence with the fragment ticket flow: read `#t=`, `history.replaceState` before any request, exchange once, keep the session in `sessionStorage`, expired-link state that keeps drafts
- [ ] 4.3 Plain-decision free-text card; lite-markup situation renderer (paragraphs, bullets, ordered lists, paired bold, backticks) using text nodes only
- [ ] 4.4 Submit: send only decided cards with `expected_updated_at`; apply per-item outcomes (answered, conflict refresh with draft kept, closed, invalid); "N answered, M need attention" summary; `focus` scroll and highlight
- [ ] 4.5 Draft persistence in `localStorage` keyed by role, item and `updated_at`; 20 s visible-only poll with a Refresh banner that never touches a card under edit
- [ ] 4.6 `GET /decide` handler plus the exact-path auth skip; embedded with the other static assets
- [ ] 4.7 Tests: Go test that the page is served unauthenticated with no board data and no external URLs (pattern of `static_spa_refs_test.go`); page-logic tests for the pure functions (markup renderer, answer payload builder, outcome application, draft keying) run through the repo's existing JS test path or the Playwright `argus-test-server` harness if it can serve the page; hostile-markup rendering test; manual checks at phone width and in dark mode

## 5. Service worker and shell

- [ ] 5.1 Add a network-only branch for `/decide` to `internal/api/static/sw.js`; keep `/decide` out of `SHELL_ASSETS`
- [ ] 5.2 Bump `SW_VERSION` from `argus-shell-v94` to `argus-shell-v95`
- [ ] 5.3 Test (`internal/api` static tests): `sw.js` bypasses `/decide`, `SHELL_ASSETS` excludes it, the version string changed

## 6. TUI (`internal/tui`)

- [ ] 6.1 Keymap: add `ActBoardDecide` (`hera_board.decide`) to `CtxHeraBoard` in `defaultSpecs`, `actionLabels` ("answer decisions in browser") and `contextOrder`; default `o`; add the `keymap.Resolve` case in `handleBoardKey`; verify no default collision
- [ ] 6.2 Board hint row: focused hint gains `o decide` (label from the active keymap) before `p plan`; keep the narrow-pane clip
- [ ] 6.3 Render structured decisions: body, blank line, Options list with Upside/Downside sub-bullets and a recommended marker, Recommendation line, generated as markdown and passed through `boardBodyLines`; collapsed headers unchanged
- [ ] 6.4 `OnBoardDecide(roleID, focusItemID)` callback on `HeraPage`; App implementation uses the local REST client (as `newLocalMaintenanceClient` does) to mint the link, builds `base + path + "#t=" + ticket`, opens it through a test-seamed opener (`open`, as `links.go`); notices for no open decisions and API disabled; inert in remote mode; selection, expansion, read state and composer untouched
- [ ] 6.5 `uxlog` `[board]` lines for open attempts, notices and failures (never the ticket)
- [ ] 6.6 Tests: `help_test.go` (`TestHelpModal_Draw`) asserts the new row; keymap default/collision and rebind tests; board render tests for structured decisions at several widths including wide runes; key tests with a fake link client and fake opener (structured row, fyi row, no selection, no open decisions, API disabled, remote); `r` still composes the plain reply; a `SimulationScreen` smoke test pressing `o` through the real event loop
- [ ] 6.7 Mirror the key into the README keybinding table

## 7. REST client parity bookkeeping

- [ ] 7.1 Record the macOS follow-up `add-mac-board-decisions` and the web `add-web-board-view` follow-up as named Non-Goals in `design.md` (done at spec time) and as tracked tasks when this change is approved
- [ ] 7.2 Confirm `apistore`/`apiclient` need no change (the TUI Hera page is inert in remote mode); add a compile-time or contract test if the new REST types are shared

## 8. Coordinator guidance and docs

- [ ] 8.1 Update the `hera_board_*` tool descriptions, the builtin `hera` skill (`internal/skills/builtin/hera/SKILL.md`) and the coordinator orientation text with the structured-decision rule, the press-`o` hint, the one-summary-message semantics, still-open follow-up and the Something-else-note rule; re-record any skill digest or pin test that fails
- [ ] 8.2 Gotchas in `context/knowledge/gotchas/`: messaging (one self-addressed summary per submit, atomic with resolutions, no dropped-notice duplicate, per-item best effort), web-remote (standalone `/decide` page, fragment ticket flow, decision auth class and allowlist, sw bypass and version bump), keybindings (`o` on the board, why not `d`/`a`), misc (update-omitted-options keeps structure)
- [ ] 8.3 README Reference appendix: MCP tool parameters, the four REST endpoints, the keybinding row; leave the marketing half alone
- [ ] 8.4 Update `context/knowledge/index.md` rows for the touched gotcha files

## 9. Verification and archive

- [ ] 9.1 Manual end to end: post a three-decision structured round through MCP, press `o`, answer two (one "Something else" with a note), submit, confirm two items resolved, one open, and exactly one `Aaron (board)` message in the coordinator inbox listing the two answers and the open id
- [ ] 9.2 Manual: edit a decision after the page loaded and submit; confirm the conflict card, preserved draft and unaffected other answers
- [ ] 9.3 Manual: confirm no ticket or session appears in `ux.log`, `ps` after launch, browser history, or the address bar after load
- [ ] 9.4 `make pre-pr` clean before opening or updating the PR
- [ ] 9.5 Run `/ralph-review add-board-decision-page` and `/spec-audit --pre-archive add-board-decision-page`, resolve blocking findings
- [ ] 9.6 Archive in the same PR before merge: `openspec archive add-board-decision-page --yes` (manual merge-and-move fallback if the CLI is absent), inspect the archive diff for every new requirement and scenario, and commit it as the PR's final completion commit
