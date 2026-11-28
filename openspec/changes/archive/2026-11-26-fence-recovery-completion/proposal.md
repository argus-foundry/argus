# Fence recovery completion

## Why

A terminal recovery can overlap a newer view or stall while reading a snapshot. Hera nuke ownership can also outlast a launch lock or collide with a generic delete path.

## What Changes

- Cancel superseded PWA recovery and enforce its deadline through snapshot loading.
- Retry deferred nuke candidates regardless of whether immediate archive succeeded.
- Distinguish a missing exit receipt from a failed lookup.
- Preserve logs and artifacts when reclaim refuses bulk delete and release completed standalone reclaim ownership.
