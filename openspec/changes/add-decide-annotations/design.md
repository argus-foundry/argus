## Context

The shipped rail-style page at commit 7fa00e96c5c6283fe8df56aacdf1fc8970f204aa is an ancestor of this branch. The archived add-decide-fyi-and-phone change and current base specs establish rail navigation, one global submission, per-role endpoint batches, 4 KiB UTF-8 notes, 64 KiB Hera messages and durable FYI receipts/outbox. Decision answers currently use resolved-item replay rather than action receipts. The prototype copies the approved rail mockup and changes only the fictional demonstration.

## Goals / non-goals

**Goals:** Let Aaron attach precise feedback to passages in decisions and FYIs, review it with quoted context and send it once with existing actions; preserve it through reload, revisions and uncertain delivery.

**Non-goals:** Annotation UI in the TUI board view; annotating subject lines, headings, option labels or recommendation reasons; arbitrary text-range selection; threaded discussion; publishing this mockup; production code in this phase. Native Mac app entry and annotation UI are the named follow-up `macos-decide-annotation-parity`; Mac currently has no Decide entry. TUI/phone launch keep using the shared web page.

## Decisions

These nine coordinator decisions are defaults for Aaron's review, and Aaron can override them.

1. **Units:** Each rendered body paragraph and each bullet or numbered-list line, including nested lines, gets its own anchor. A parent list line excludes descendant text. Structured options additionally expose summary, upside and downside as separate blocks. Headings and subjects are excluded. Use revision-local paths (body paragraph/list path or option ID plus field) and the rendered plain-text quote, not matching text alone, so repeated bullets remain distinct. A quote is at most 512 UTF-8 bytes with a visible ellipsis; the server derives it from the submitted revision and anchor rather than trusting client text. Inline bold/code/links keep their existing safe rendering. Paragraphs spanning source lines remain one rendered block.

2. **Interaction:** Hover/focus reveals a subtle comment affordance. Click/tap on block text or its affordance opens a labeled inline textarea below that exact block; Enter on the focused block does the same. Save queues feedback, Cancel discards only the editor changes, and saved blocks show one marker plus Edit/Remove. One saved annotation per block; at most 20 per item. Option annotation controls must be siblings of radio labels, never nested interactive controls inside labels: commenting cannot select an answer. Link clicks/Enter follow the existing safe link and never open the editor; annotation gestures never activate a link. Exclude annotation regions from tab-swipe handling, preserve selection and text editing, restore focus on close. On phone, show the affordance persistently with a 40px tap target.

3. **Kinds:** Both decisions and FYIs support the same blocks and editing flow.

4. **One response:** Saving a nonempty annotation queues that item; editor typing alone does not. The existing Review queued groups annotations under their item with quoted blocks and comments. The single existing Submit actions sends them alongside choice/note/action; Copy responses includes the same context. Remove the last annotation from a comment-only decision to unqueue it unless an explicit comment action with a valid note remains.

5. **No answer:** An annotation-only decision queues Comment, leaves choice empty and stays open after delivery (plain decisions too). Selecting an option turns it into an answer carrying annotations. Something else and plain answers still require their normal answer note; annotations do not substitute for it. On FYIs, saving the first annotation defaults an unset action to Comment. Dismiss plus annotations becomes Comment and dismiss, with an explicit visible label; no silent loss of feedback. Removing annotations does not silently resolve or dismiss anything. A comment may be satisfied by a nonempty note or one saved annotation. Counts and finish handling classify decision comments as comments, not answers; commented decisions stay in open counts.

6. **Delivery:** Extend existing /answers with action ID, action (answer/comment) and annotations; absent action retains ordinary answer behavior. Annotation-bearing answers and all decision comments require durable action receipts. Extend /fyi-actions and its existing receipt path with annotations. No new submit control or transport. Each annotation has anchor, derived quoted text and full comment; bind it to role/item/expected_updated_at. Commit receipt, item mutation if any and exactly one existing summary per endpoint batch atomically. Add quote/comment entries inside that summary; do not send a message per annotation. Existing mixed submissions may produce separate answer-route and FYI-route summaries; this change does not promise a single message across all endpoint batches. Preserve full annotations in receipts, including for resolved answers; extend the existing privileged receipt read path to retrieve decision response receipts as well. Rail-board sessions retain write/replay authority only, never history access. Comments do not change item revision/read state. Identical receipt replay precedes revision/closed checks and emits nothing new. New feedback on closed items is invalid, never reported delivered as already_resolved.

7. **Drafts:** Use existing guarded localStorage response records, never credentials, keyed by role/item/revision. Persist annotations and original quote/path with note drafts and frozen sent payloads before sending. Unknown outcomes lock payload edits/removal until reconciled using the original action ID. On conflict or accepted refresh of a revised item, retain old notes/annotations in a clearly labeled recoverable stale draft, clear pending answer/action and require review plus explicit reanchoring or removal; never attach an old quote to new content automatically. A wholly stale draft does not submit until reconfirmed. Copy remains available for recovery. Storage failures remain visible and do not break rendering. Successful receipts clear only acknowledged annotations.

8. **Caps:** Each comment is non-whitespace and at most 4096 UTF-8 bytes, independently of the general note. Maximum 20 annotations per item; 512 UTF-8 bytes per derived quote. Validate both client/server, including multibyte boundaries and duplicate/unknown anchors. Show limits before Save; errors retain editor text. Keep 16-item batch limit, adjust bounded request decoding for worst-case annotation size including JSON escapes, and keep summaries at 64 KiB by evenly budgeting general notes and annotation quotes/comments. Every omitted/shortened entry gets a receipt recovery pointer; full feedback remains retrievable. These are bytes, not JavaScript character counts.

9. **Guidance:** Update builtin Hera board guidance, board-tool description and coordinator spawn/reclaim orientation: annotations arrive as quote/comment pairs; address each one, retrieve full receipts when truncated, distinguish feedback-only open decisions from answered decisions, and keep settled follow-up reflected on the board. This is a spawn-surface change and requires the inventory/contract checks.

## Risks / tradeoffs

- Clickable text inside selectable options could accidentally answer a decision → separate comment targets from radio labels and test pointer/keyboard propagation.
- Revision-local anchors become obsolete after edits → retain stale text and require explicit reanchoring rather than guessing.
- Twenty full comments per item exceed a Hera summary budget → durable full receipts and bounded quoted summaries with pointers.
- A decision resolved elsewhere cannot accept fresh feedback → return invalid for undelivered annotations, preserve drafts and explain the closed item.

## Migration plan

After approval, write delta-derived tests first, extend the existing receipt model to decision actions and annotation arrays, then implement renderer and queued-state changes. Existing notes/actions without annotations retain their behavior. Follow repository breaking-change policy rather than adding legacy migration layers. Validate and archive into base specs in the eventual implementation PR before merge; this spec-only branch stays active.

## Open questions

Aaron's approval is pending. No coordinator decision is rejected; the notable clarification is that one existing summary means one per existing role/endpoint batch, not one global message for a mixed multi-board submission.


## Review evidence

- Strict validation: openspec validate --all --strict, 94 passed and zero failed.
- Playwright: editor, annotated FYI and quoted review fit 1200px and 390px in Light and Dark without horizontal scrolling. Desktop and phone screenshots visually inspected.
- Interactions checked: Enter opens the editor; Save, Cancel and Remove work; annotating an unselected option does not change its radio; annotation-only decision submission reports a comment and leaves the item open; FYI Dismiss becomes Comment and dismiss when an annotation is saved.
- Mockup is fictional, local and unpublished; no production source changed.
