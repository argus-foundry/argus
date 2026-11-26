# Coordinate voice stop with the session owner

## Why

A confirmed stop can overlap a launch or restart. The session owner can resolve the stop under its runner lock and keep the task state consistent with the selected session.

## What Changes

Voice stop uses a session-owner RPC that waits for or refuses a pending launch or restart. The runner fences replacement requests once it selects a session to stop. The TUI, web app, and macOS app use their existing REST command and response schemas.
