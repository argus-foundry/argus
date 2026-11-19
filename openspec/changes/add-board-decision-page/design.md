## Context

A board item (`hera_board_items`, `internal/db/hera_board.go`) is `kind` (`decision`|`fyi`), a one-line `subject`, a free-text `body`, read state, and a resolution (`resolved`, `resolved_by`, `resolution_note`). Writers are the MCP tools `hera_board_post/update/resolve/list` (coordinator-only), the TUI board (`internal/tui/hera/board.go`: Enter expands, `r` pastes `Re: <subject> (#id)` into the coordinator composer, `x` dismisses) and three REST routes (`GET .../board`, `PUT .../read-state`, `POST .../dismiss`). Every human mutation is revision-checked with `expected_updated_at`, and a human dismissal of a decision writes a self-addressed `hera_messages` row in the same transaction, which the notify tick delivers through `PendingHeraBoardNotices` / `hera.DeliverBoardNotices` (labelled `Aaron (board)`).

Aaron's `/decision` skill (`~/.claude/skills/decision/`) is the target UX. Its page has a sticky tally bar with one segment per decision, one open section per decision (question, situation paragraphs, option cards with label/summary/upside/downside, a Recommended pill plus "why Claude recommends it", a dashed "Something else" card, an optional note box), a "take the recommendation for the N undecided" button, a "copy answers" fallback, and local-storage drafts. All text renders with `textContent` plus backtick spans; there is no `innerHTML`. In the skill, answers return through an artifact database or a pasted block.

The REST server (`internal/api`) binds `127.0.0.1` plus the Tailscale IP, serves the SPA at `GET /` without auth, and authenticates every `/api/*` route with the master token, a device token, or a scoped plugin token. The SPA keeps its token in `localStorage`. The service worker caches non-`/api/` GETs.

## Goals / Non-Goals

**Goals:**

- Structured decisions live on the board item, optional, so plain decisions are untouched.
- One TUI key opens a daemon-served page showing every open decision on that coordinator's board as one round.
- Submitting records each answer durably and sends the coordinator exactly one summary message.
- No credential in shell history, URL query strings, access logs or `uxlog`.
- Partial submits, "something else" and mid-round coordinator edits behave predictably.

**Non-Goals:**

- **macOS UI.** The macOS Hera tab stays read-only. Named follow-up `add-mac-board-decisions`: an "Open decisions in browser" button that calls the same decision-link endpoint and opens the URL, plus eventually a native answer view. The endpoints in this change are client-agnostic, so the follow-up needs no daemon work.
- **Web SPA board view.** The SPA still shows no board. Named follow-up `add-web-board-view`: a board list in the Hera tab with an "Open decisions" button. The standalone page is the web answer surface for this change.
- **`/decision` skill integration.** When the `/decision` skill runs inside an argus sandbox it could post a board round instead of publishing a claude.ai artifact. That is a separate change in Aaron's skill repo and is out of scope here. This change only has to make the board capable of it (the structured fields mirror the skill's JSON shape on purpose).
- **Live push to the page.** The page polls. SSE push (`hera.board_changed` is not forwarded from TUI-originated writes, see ENG-1125) is a follow-up.
- **Multi-coordinator rounds.** A round is one coordinator's board.
- **Editing items from the page.** The page answers; only the coordinator edits.
- **Wiring OpenSpec or any spec tooling into CI.** Specs stay local docs.

## Decisions

### D1. Open questions, with recommendations

The three open questions from the brief, plus two the design surfaced. Each has a recommendation that the rest of this change implements. Aaron can overrule any of them at approval.

- **Q1. Which TUI key?** Recommend **`o`** ("open in browser"), `hera_board.decide` in `CtxHeraBoard`.
  - Free: the board context uses `p r x j k`; the globals are `q ? 0 1 2 3` and `ctrl+*`; `o` already means "open" in the file and diff panels, so the mnemonic is consistent. `ctrl+o` is the global open-repo key and stays distinct.
  - Alternatives considered: `d` (reads as "dismiss", collides with the `x` mental model), `a` ("answer"; collides with the rail's archive key, which is a different context but the same screen), `ctrl+o` (taken).
  - Behavior: `o` works on any row, including `fyi` rows and an empty selection. If the board has no open `decision` items it shows a one-line notice and opens nothing.
- **Q2. Auth approach?** Recommend a **single-use launch ticket in the URL fragment, exchanged for a scoped in-memory session** (D3).
  - Alternatives: put a device token in the URL (rejected: a broad, long-lived credential would land in browser history, and a `?token=` query reaches access logs and `Referer`); reuse a scope-token via `api_tokens` (rejected: existing handlers admit "master or any scope token" for MCP-tool and plugin-view registration, so a board token would silently gain those powers).
- **Q3. New SPA route or standalone HTML?** Recommend a **standalone embedded `decide.html`** served at `GET /decide` (D4). The SPA token in `localStorage` is a master or device token and the page deliberately must not need it; the standalone file can also stay close to Aaron's `template.html`, which makes future drift easy to reconcile.
- **Q4. Do plain free-text `decision` items appear on the page?** Recommend **yes**, as a free-text card (the body as situation, a required note as the answer, recorded as choice `other`). That makes the page the whole decision queue and gives the "single decision with a lot of copy" case a richer reading format. `fyi` items never appear. The alternative (structured items only) leaves a coordinator that posts a plain decision with no way to use the page and makes `o` on such an item a dead end.
- **Q5. Daemon bounce while the page is open?** Recommend **in-memory tickets and sessions**. A bounce ends the session; the page keeps drafts in `localStorage`, shows "This link expired. Press `o` in Argus to reopen it", and offers Copy answers. Persisting sessions would add a table and a revocation story for a window that is minutes to hours long.

### D2. Where the structured data lives, and what counts as a material edit

Two additive columns on `hera_board_items`:

- `structured TEXT NOT NULL DEFAULT ''` holds JSON: `{"options":[{"id","label","summary","upside","downside"}],"recommended":"<id>","reason":"<text>"}`. Empty string means a plain item.
- `resolution_choice TEXT NOT NULL DEFAULT ''` holds the chosen option id, `other`, or empty (legacy dismissals and agent resolutions).

The existing `body` is the situation text. There is no separate `situation` field: one source of truth, and the TUI already renders `body`. Guidance for structured decisions is that `body` states the situation and the options live only in `options`, so the two never duplicate.

A JSON column rather than an options table because an option set is always written and read as a unit, is capped at four options, never queried by option, and must change atomically with `updated_at`. Validation is in `internal/db` beside `validBoardContent`:

- `structured` is valid only on `kind=decision`; two to four options; option ids match `^[a-z0-9][a-z0-9-]{0,39}$`, are unique, and are never `other`; `label` is required and at most 80 characters; `summary`, `upside`, `downside` and `reason` are at most 400 characters; no control characters other than newline in `reason`; `recommended`, when present, names one option; the serialized JSON is at most 16 KiB.
- Resident approval items reject structured data, and approval items are already immutable through update.

`updateHeraBoardItem` already treats a change to kind, subject or body as material (marks unread, bumps `updated_at`). A change to `structured` is material for the same reason: Aaron may be looking at a page that shows the old options. That bump is what makes the compare-and-set check in D5 fire.

MCP parameters are `options` (array), `recommended` and `reason`. `hera_board_update` currently replaces kind, subject and body wholesale. For structured data that is a trap: a coordinator that only rewrites `body` would silently erase the options. So an update that **omits** `options` keeps the stored structure (and still validates `recommended` and `reason` against it if sent), and `options: []` clears it. Decoding distinguishes absent from empty with a `*[]option` or `json.RawMessage`. A drift test pins the advertised schema to the decode struct, the same guard the `hera_plan` tools use.

### D3. Launch ticket, exchange and decision session

```
TUI ──POST /api/hera/roles/{role}/board/decision-link (master/device token)──► daemon
     ◄── {path:"/decide?role=12&focus=40", ticket:"…", expires_at}
TUI ── open http://127.0.0.1:7743/decide?role=12&focus=40#t=<ticket> ──► browser
browser ── GET /decide (static shell, no data, no auth) ──► daemon
page JS reads location.hash, replaces the URL with history.replaceState (drops the fragment)
page ── POST /api/hera/board/decision-session {ticket} ──► {session, role_id, expires_at}
page ── GET  /api/hera/roles/12/board/decisions   (Bearer <session>)
page ── POST /api/hera/roles/12/board/answers     (Bearer <session>)
```

- **Ticket:** 256-bit random, held in daemon memory as a hash, bound to one `role_id`, valid 90 seconds, consumed on first exchange. A second exchange returns 401.
- **Session:** 256-bit random, held in memory as a hash, bound to the same `role_id`, 8-hour absolute lifetime, bearer-presented. It is held in `sessionStorage` (not `localStorage`) so closing the tab ends it.
- **Why a fragment:** a URL fragment is never sent to a server, so it cannot reach access logs, the daemon's own logging, or `Referer`. `history.replaceState` then removes it from the visible address and from the history entry the browser keeps.
- **Residual exposure, accepted:** `open <url>` puts the URL (including the ticket) in that process's argv for a few milliseconds, and the browser's history may hold the ticketed URL until `replaceState` runs. The ticket is single-use, 90 seconds long and scoped to one board, so a leaked ticket that has been used is worthless and an unused one expires almost at once.
- **Allowlist:** the middleware tags a session `X-Argus-Auth: decision:<role_id>` and permits only `GET .../board/decisions` and `POST .../board/answers` for that same role id. Anything else returns 403. `requireMaster` and every other handler treat the tag as unknown, so no existing gate widens. Master and device tokens may call the same two endpoints for any role, which is what the macOS follow-up will use.
- **No secret in logs:** `uxlog` lines for this flow log role id, item ids, counts and outcomes only. Tickets and sessions are never logged, never stored in the database, and never placed in an error string.
- **Remote and Tailscale:** the TUI builds the URL from the base it already uses (loopback for a local daemon). The Hera page is inert in `--remote` mode, so the key does not exist there; the macOS follow-up covers a remote daemon.

### D4. The page

`internal/api/static/decide.html`, embedded and served by a new `handleDecidePage`. `/decide` joins the auth middleware's skip list as an exact path (like `/share`): the shell contains no data. It is adapted from Aaron's `template.html`: same layout, tally bar, option cards, Recommended pill with the reason, dashed "Something else" card, optional note, "take the recommendation for the N undecided" button, Copy answers fallback, and `textContent`-only rendering.

Differences from the template:

- Data comes from `GET .../board/decisions` instead of an inline JSON block; submit is a `POST` instead of the artifact database.
- No external requests. The template links Google Fonts; the daemon page uses the system font stack, so it works offline over Tailscale and sends nothing off-machine.
- The situation renderer understands the same lite markup the TUI board does (paragraphs, `- ` and `1. ` lists, paired `**bold**`, backtick spans), because `body` is written for the TUI. Still no `innerHTML`.
- Plain decisions render as a free-text card: situation, then a note that is required to answer.
- `focus=<item id>` scrolls to and highlights that card; the round still contains every open decision.
- A **Submit answers** button replaces "go back to Claude and say done". Submit is enabled when at least one card has a choice (and "something else" cards have a note). The tally reads "N of M decided".
- Drafts are kept in `localStorage` under `board-decision:<role>:<item>:<updated_at>`, so a draft is discarded automatically when the coordinator revises that item.
- The page polls the decisions endpoint every 20 seconds while visible. A changed or new item raises a banner ("Claude updated 1 decision and added 1") with a Refresh button; it never replaces a card under edit.

`/decide` is not added to `SHELL_ASSETS` and `sw.js` gains a network-only branch for `/decide` (see D7).

### D5. Submit: per-item outcomes, atomicity, staleness

Request: `{"answers":[{"item_id":40,"expected_updated_at":"…","choice":"auto-retry"|"other","note":"…"}]}`. Only decided cards are sent; undecided cards are simply absent and stay open.

A new `db.AnswerHeraBoardItems(roleID, answers)` applies the batch in one transaction under the existing `d.mu`:

- For each answer it loads the item and decides one outcome: `resolved`, `conflict` (the item's `updated_at` differs from `expected_updated_at`: the coordinator edited it after the page loaded), `already_resolved`, `not_found` (wrong role, missing, or not a `decision`), or `invalid` (unknown option id, `other` with an empty note, a resident-approval item, note over 4 KiB).
- Non-`resolved` outcomes change nothing and never block the others. This is deliberately **per-item best effort inside one transaction**, not all-or-nothing, because partial submits are a first-class case.
- Resolved items set `resolved=1`, `resolved_by='human'`, `resolution_choice`, `resolution_note`, and bump `updated_at`.
- If at least one item resolved, the transaction inserts **one** self-addressed `hera_messages` row (`from_role_id = to_role_id`, `delivery_mode = pending`) summarizing every resolved answer. If none resolved, no message is written. Commit covers items and message together, so a crash can never leave an answered item with no notice.
- After commit, `hera.board_changed` is emitted once per resolved item, as the other resolvers do.

Response: HTTP 200 with `{"results":[{"item_id","outcome","item"?}],"unread_count":N}`. A `conflict` result includes the current item so the page can show what changed, keep Aaron's draft text, and let him re-answer. Malformed bodies return 400 `invalid_request`; an unbound or unknown session returns 401/403; an unknown role returns 404.

The summary message (delivered by the unchanged `DeliverBoardNotices` path) reads, in this shape:

```
Aaron answered 3 board decisions. Act on these; each is already resolved.

#40 When the nightly export fails, retry or wait?
  Choice: Retry automatically (the recommendation)
  Note: none

#41 Should the morning report stay a spreadsheet?
  Choice: Something else
  Note: Keep the sheet, but add one chart image.

Still open: #42, #43
```

with a `tldr` of "Aaron answered 3 board decisions". The note cap and the per-round item cap (at most 16 items per submit, 4 KiB per note) keep full notes durably on each item. Summary notes SHALL be budgeted evenly across answered items to keep the message within 64 KiB. A shortened note ends with `… [truncated — full note on board item #N; hera_board_list include_resolved=true]`; notes under budget remain untouched.

### D6. TUI changes

- **Rendering:** a structured decision renders `body`, a blank line, an **Options** list (`- **<label>**` plus `(recommended)` on the recommended one, then `  - Upside:` and `  - Downside:` sub-bullets), and a `**Recommendation:** <reason>` line. It is generated as markdown and fed through the existing `boardBodyLines`, so wrapping, bold and hanging indents come for free.
- **Key:** `ActBoardDecide` (`hera_board.decide`, default `o`, label "answer decisions in browser") in `CtxHeraBoard`; a `keymap.Resolve` case in `handleBoardKey`; the focused hint row becomes `j/k move · ⏎ expand · r reply · x dismiss · o decide · p plan · ctrl+q back`. The help overlay is generated from the keymap. `help_test.go` and the README keybinding table are updated in the same PR.
- **Launch path:** `p.OnBoardDecide(roleID, focusItemID)` is an App-owned callback like `OnBoardRead` and `OnBoardDismiss`. The App calls the decision-link endpoint through the same local REST route the cleanup popup uses (`newLocalMaintenanceClient`: loopback base URL plus the `~/.argus/api-token` master token), builds `base + path + "#t=" + ticket`, and opens it with the same `open` seam `links.go` uses. A test seam replaces both the client and the opener. If the REST API is off, the footer says "Turn the API on in Settings to answer decisions in the browser".
- **Reply is untouched.** `r` still pastes the plain reply prefix, for structured and plain items alike.

### D7. Frontend parity and the service worker

AGENTS.md makes a change to the REST-exposed surface a trigger to evaluate all three frontends.

| Surface | This change |
| --- | --- |
| TUI | Launcher key `o`; structured rendering. |
| Web | New standalone `/decide` page (the answer surface). The SPA shell is unchanged, so no SPA edit. |
| macOS | Named follow-up `add-mac-board-decisions` (see Non-Goals). The macOS Hera tab was already a documented read-only gap. |

`sw.js` changes (a network-only branch for `/decide`, so the worker never serves a cached page or caches the ticketed URL), so `SW_VERSION` is bumped from `argus-shell-v94` to `argus-shell-v95` in the same PR.

### D8. Guidance to coordinators

The `hera_board_post` / `hera_board_update` descriptions, the builtin `hera` skill and the coordinator orientation add: when a decision has distinct options, post it structured (two to four options, always a recommendation with a reason), keep `body` to the situation, and tell Aaron to press `o` on the board for a round of several. The existing scanning-format rule (lead sentence, `- **Label:** detail` bullets) still applies to plain items and to a structured item's situation. After the notice arrives the coordinator treats each listed answer as already resolved, follows up on "Still open" items, and handles a "Something else" note as the answer. A digest or pin test for the builtin skill is re-recorded if it fails.

## Risks / Trade-offs

- **Ticket in `open`'s argv.** Visible briefly to other processes on the same machine. Mitigated by single use, a 90-second life and a one-board scope; the worst case is another local process answering Aaron's decisions in that window, and a local process could read the SQLite file anyway.
- **In-memory sessions and daemon bounces.** An answer typed into an expired page is not lost (drafts persist, Copy answers works), but Aaron must reopen from the TUI. Accepted; bounces are infrequent and the page says exactly what to do.
- **Per-item best effort means a submit can resolve some items and conflict on others.** The response and the page make this explicit per card. The coordinator's message lists only resolved items plus "Still open", so it never acts on a conflicted item.
- **Plain decisions on the page** record choice `other`, which is accurate but coarse. Coordinators that want a recorded choice use structured options.
- **Page drift from Aaron's template.** The two now diverge deliberately (fetch instead of artifact DB, no web fonts, richer markup). Keeping them identical was never a goal; the data shape (id/label/summary/upside/downside/recommended/reason) is what must stay compatible for the later `/decision` integration.
- **`o` on a board with only `fyi` items** does nothing visible beyond a notice. Intentional; the alternative is a page with no cards.
- **Coverage floor.** New daemon code (session store, batch answer, handlers) and the page's logic need tests to hold the 88% gate; `tasks.md` lists them explicitly.
