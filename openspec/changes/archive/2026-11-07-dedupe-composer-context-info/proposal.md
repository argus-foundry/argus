# Deduplicate composer context and task Info

## Why
Account and historical role token totals repeat task Info inside the composer context popover.

## What Changes
Remove those duplicate composer rows and its unused role totals and roster reads. Keep the shared roleTokenTotals formatter for Info. Retain context measurements, Claude account details and usage limit windows.

## Impact
Only the optional web workspace changes. REST, TUI, shipped web and macOS behavior stay unchanged because this changes only the optional workspace presentation.
