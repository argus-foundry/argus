# Fix Pi dash-prefixed prompts

## Why
Pi treats a shell-quoted prompt beginning with a dash as an option rather than a message. Markdown frontmatter (`---title:`) and literal `--title "..."` prompts are silently lost. Pi does not honor an end-of-options separator.

## What Changes
- Prefix a newline to dash-prefixed positional prompts for the Pi family, preventing CLI option interpretation while retaining all original prompt content.
- Leave ordinary prompts, configured prompt-flag delivery, resume behavior, and other backend families unchanged.
- Add regression tests for frontmatter, long-option-like and short-option-like prompts, shell quoting, and unchanged ordinary delivery.

## Non-Goals
- No REST contract or frontend changes: TUI, web, and macOS task creation share the daemon command builder and all receive the fix.
- No change to Pi's documented leading-`@` file-inclusion behavior.
- No interpretation or execution of handoff content.

## Verification
Reproduced against the installed Pi `parseArgs`: dash-prefixed input produces no messages and an unknown flag; newline/whitespace-prefixed input produces a message. Add command-construction regressions and run agent package tests.
