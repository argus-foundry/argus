## Why

Aaron needs to comment on individual passages in long decisions and FYIs, keeping each comment tied to the text it addresses and submitting all feedback in the existing round.

## What changes

- Make body paragraphs and individual list lines, including nested lines, annotatable; include structured option summaries and tradeoffs.
- Add inline comment editing, saved markers, keyboard and phone interaction, and quoted annotations in Review queued and Copy responses.
- Queue annotation-only feedback without resolving decisions; include annotations with answers and FYI actions.
- Extend durable receipts, revision checks and the board-notice outbox; preserve drafts through retry, uncertainty and conflicts.
- Teach coordinators to address every annotation in the existing board guidance and spawn orientation.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `board-decision-page`: Block annotation interaction, queued feedback and draft recovery.
- `coordinator-board`: Durable annotations and coordinator guidance.
- `rest-api`: Annotation-bearing answers, decision comments and FYI actions through existing routes.

## Impact

Future implementation touches standalone decide rendering, API validation/auth, board receipts and summary budgeting, builtin Hera guidance and spawn-surface inventory, tests, gotchas and README Reference. No new transport or dependencies. TUI annotation UI is excluded; native Mac entry remains the named follow-up `macos-decide-annotation-parity`. This is a spec and unpublished mockup only; implementation awaits Aaron's approval.
