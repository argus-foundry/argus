## Decisions

- **Probe root cause.** The live log's dead verdict carried a non-zero PID, which only a cached handle can produce; reproduced against a real supervisor harness by detaching the client and asking the detached daemon. Fix at the answering site, not the client: trust a dead handle only after the supervisor confirms.
- **Un-end the same binding row on revive** instead of inserting a new one, so the role identity and accrued cost survive; it is limited to the latest accepted-ended coordinator binding whose role and orchestrator are neither archived nor nuked and which has no live twin.
- **Restore at every resume entry** (hera revive/auto-revive, TUI resume incl. third-Enter, REST resume/restart), not only the revive primitive, so a plain Enter on an accepted sub-coordinator also gets its hat back. The daemon-bounce reattach deliberately does not restore: a live accepted session there was not revived.
