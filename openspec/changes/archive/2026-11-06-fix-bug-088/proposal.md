# fix-bug-088

BUG-088: the Tasks rail's selected task row had the pink name but no `›` gutter caret (only project/archive rows drew it), so it did not look like the Projects rail's selection. Fix draws the shared `theme.RailCaret` in the gutter with the same style.
