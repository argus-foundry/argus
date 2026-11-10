---
name: hera-remind-me
description: Put a follow-up from this conversation (e.g. "when we release, do X") on the Hera coordinator board so Aaron sees it later. Use when Aaron runs /hera-remind-me. A coordinator posts the item itself; a worker or freelance agent relays it to its coordinator; a session with no Hera binding has no board.
allowed-tools: mcp__argus__hera_board_list, mcp__argus__hera_board_update, mcp__argus__hera_board_post, mcp__argus__hera_send
---

# Put a reminder on the coordinator board

Aaron wants something from this conversation kept somewhere durable and visible: his coordinator's board in the Argus Projects tab. Chat scrollback is lost when the session recycles. The board isn't.

## Arguments

- `$ARGUMENTS`: optional free text naming what to capture, e.g. `the release step` or `bump SW_VERSION before we ship`.
  - If it's present, capture what it names. You can use conversation context to fill in details.
  - If it's empty, capture the most recent actionable follow-up you or Aaron mentioned. A deferred "when X, do Y", a "remember to…", or a "don't forget…" are typical. If nothing in the recent conversation qualifies, or two candidates are equally recent, ask Aaron which one he means and stop.

## Context

- Current directory: !`pwd`

Pass this directory as `cwd` on every tool call below.

## Step 1: draft the item

Write it so it makes sense weeks from now without this conversation:

- **subject**: one line, under 80 characters. Start with the trigger when there is one: `On release: bump SW_VERSION in sw.js`.
- **body**: a few short lines. Include:
  - **Do**: the concrete action.
  - **When**: the trigger condition, or "no trigger, at Aaron's convenience".
  - **Why**: one sentence on why it matters or what breaks if it's skipped.
  - **Refs**: any PR, branch, task, file path or command it depends on. Leave this out if there are none.
- **kind**: `decision` if Aaron has to choose something before it can happen. Otherwise `fyi`.

Don't invent details. If the trigger or action is unclear, write what was actually said.

## Step 2: route by role

Call `mcp__argus__hera_board_list(cwd)`. The result decides which path to take.

If it errors because this task holds more than one live Hera binding, ask Aaron which orchestrator he means, then retry with `orchestrator` set. Pass that same `orchestrator` on every later call.

### It succeeds: you are a coordinator

1. Look at the open items it returned. If one already covers the same follow-up, call `mcp__argus__hera_board_update(cwd, id, kind, subject, body)` with a merged rewrite. Update replaces all three fields, so carry forward anything still true from the old item.
2. Otherwise call `mcp__argus__hera_board_post(cwd, kind, subject, body)`.
3. Reply to Aaron in one line: `Posted #<id>: <subject>` or `Updated #<id>: <subject>`.

### It errors with "board tools require a coordinator binding": you are a worker or freelance agent

Do not call any board tool. Only your coordinator can write to its board. Relay the item instead:

```
mcp__argus__hera_send(
  cwd: "<pwd>",
  tldr: "Aaron asked to board: <subject>",
  status: "<your current role status, usually working>",
  body: "Aaron ran /hera-remind-me and asked for this on your board. Please post it (or merge it into an existing item):\n\nkind: <kind>\nsubject: <subject>\n\n<body>"
)
```

Reply to Aaron in one line: `Sent to <coordinator> to post: <subject>`.

### Any other error: no Hera binding

Tell Aaron there is no board for this session because it isn't part of a Hera team, and show him the drafted subject and body so the text isn't lost. Do not post, send, or create anything else.
