# Guard task resume against Hera close-out

## Why

Resuming a finished Hera worker over REST undid close-out; the TUI requires a deliberate three-step revival.

## What Changes

REST handlers in `internal/api/handlers.go` gain the guards below; no schema change.

## Non-Goals

Web confirmation UI for forced revival (ENG-1116) and native explicit-revival UI are the named follow-ups. The TUI keeps its own three-step guard.
