## Context

The existing strip paints labels and count badges separately, making selection and label spacing ambiguous.

## Goals / Non-goals

- Goals: a continuous selected fill, a plain unselected label/count gap, and bounded narrow strips.
- Non-goals: REST changes or web Decide styling. The existing named macOS Hera board kind tabs follow-up remains unchanged.

## Decisions

Use the existing kind fill and dark badge ink for the entire selected string, including one outer space on each side. For unselected nonzero counts, retain symmetric badge padding after a separate plain label gap. Zero counts remain dim and unfilled without trailing padding. Measure each tab's actual padding when deciding whether to abbreviate. Keep a contiguous window of whole neighboring tabs around selection to avoid orphan separators. For strips narrower than the selected tab, start at its label so a leading padding cell cannot hide the selection.

## Risks / Trade-offs

- Narrow strips cannot fit all tabs: retain abbreviation and the selected window, testing every selected kind down to one cell.
- Terminal backgrounds vary: explicit kind fill and dark ink remain independent of the terminal background, tested over dark and light default backgrounds.
