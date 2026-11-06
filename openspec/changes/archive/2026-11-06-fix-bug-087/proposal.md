# fix-bug-087

BUG-087: the Tasks rail row for the task in the agent pane lost its selected color when the terminal took focus (cursor was dimmed). Spec gap: the rail highlight was unspecified. Fix keeps the selected style regardless of focus; only the border reflects focus.
