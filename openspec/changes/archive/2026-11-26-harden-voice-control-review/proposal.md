# Harden the voice command boundary

## Why

Voice commands that start, stop, resume, or write to a task carry the same authority as REST task mutations.

## What Changes

Require the Argus API master token in an Authorization header for these MCP commands. Keep the cwd-free discovery and status tools available on the local MCP server.

The TUI, web, and macOS clients keep their existing REST contract. This change affects direct MCP voice clients.
