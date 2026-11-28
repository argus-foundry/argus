# Recover owner lifecycle state

## Why

A daemon restart can interrupt reclaim finalization. Restarts may also outlast normal transport retries, and the task's process can change between an output snapshot and stream attachment.

## What Changes

Recover matching reclaim exits from the supervisor and validate Hera bindings before cleanup. Keep confirmed managed restarts tracked during prelaunch and refuse to initiate them on older owners that cannot report their successor. Tie live terminal cursors to the process and renew the snapshot when a replacement has started.
