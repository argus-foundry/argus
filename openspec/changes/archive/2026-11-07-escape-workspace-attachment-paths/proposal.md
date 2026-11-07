# Escape workspace attachment paths

## Why

Uploaded filenames can contain spaces and shell metacharacters. Sending raw paths makes a single attachment ambiguous in the agent prompt.

## What Changes

Escape individual attachment path tokens using terminal file drag-and-drop backslashes. Preserve simple paths, space separators and user text.

## Scope

Optional web workspace composer only. The original web SPA attachment formatter is a named follow-up, `escape-original-spa-attachment-paths`. TUI terminal paste already accepts terminal-produced paths; macOS is outside this formatter fix. No REST contract changes.
