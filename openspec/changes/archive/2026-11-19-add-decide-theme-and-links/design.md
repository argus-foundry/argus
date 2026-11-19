## Context

The standalone page has both palettes and a DOM-only formatted-text renderer. Its launch fragment is stripped before any fetch; session credentials remain confined to sessionStorage.

## Goals / Non-goals

**Goals:** Restore appearance before paint, tolerate unavailable storage, and make HTTP(S) references usable without interpreting HTML.

**Non-goals:** REST changes, TUI or macOS appearance changes, remote assets, credential persistence changes, or rendering links inside code spans.

## Decisions

- Place a small theme helper in the head before styles/body. A validated value sets the root data-theme attribute; System leaves palette selection to prefers-color-scheme CSS, including live system changes. Only the theme string is stored under its own key.
- Use a labeled select in the sticky bar. Apply changes even if storage writes fail.
- Linkify text segments with DOM text nodes and anchor elements. The inline renderer handles code first and recursively formats bold content, so references inside code remain literal. Strip terminal sentence punctuation from link destinations while preserving it as text.
- Keep all existing fetch and launch/session handling untouched. Anchors open only on explicit user navigation and carry noopener noreferrer.

## Risks / Trade-offs

- Unavailable storage loses persistence → render System initially and allow temporary appearance changes without throwing.
- URL punctuation is ambiguous → trim terminal sentence punctuation, retaining balanced URL parentheses where possible.
- Long references can overflow phone layouts → allow wrapping in decision text and headings.
