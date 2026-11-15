## Context

Solo forces default mode and delivers isolated inline settings. Aaron approved auto mode while keeping fixed rules. Research used the requested claude-code-guide sub-agent, official docs and installed Claude Code 2.1.325.

## Goals / Non-goals

- Goals: enforce auto without bypass, preserve fixed ask/deny boundaries, keep Solo startable when auto is unavailable, and surface actual user-input needs.
- Non-goals: a general permission-mode picker, guaranteed confirmation for every Git/gh action, changes to worker bypass mode, or REST/client contract changes. All three frontends already share resident waiting and PTY output.

## Decisions

- Force `--permission-mode auto` and inline `defaultMode:auto`; keep bypass stripping, flag allowlist, isolated settings and disableBypassPermissionsMode. Do not add `resident.permission_mode`: one enforced mode plus native safe fallback meets the request without a new config/API/UI surface or validation matrix. A configurable knob could be added later if an operator needs a persistent Manual choice.
- Delegate account/model/settings/service fallback to Claude Code, which documents that explicit auto requests start in Manual/default when unavailable. Do not run account/classifier probes or retry arbitrary failed sessions: those add latency and can replay work. Observe explicit native fallback notices in the resident PTY and dual-log through uxlog/slog once per session. Older CLIs without the auto choice require upgrade; the supported installed CLI includes auto and the v2.1.323 safe-fallback fix.
- Preserve allow/ask/deny rules. Deny beats ask, ask beats allow; narrow allows skip classifier review. Auto suspends broad arbitrary-execution allows, but restores them on leaving auto. Remove shipped bare Bash so outward shell actions remain reviewable after fallback too. No push/gh grants ship. Do not add unconditional asks: the requested behavior is classifier review, which can approve same-repo pushes and requested PR creation.
- Per coordinator direction, replace blanket Bash with a short read-only list: ls/cat/head/tail/grep/wc and git status prefixes, plus exact rg --files, find . -type f and git log/diff/show/branch commands. Exact forms avoid allowing rg --pre, find -exec/-delete, Git output/external-command flags and branch mutations. Push and gh remain outside all shipped Bash allows; these operations go to the classifier, without an unconditional ask.
- Normal auto denials are nonblocking. Explicit asks and repeated-block/context-size escalation use the ordinary numbered permission chooser. Repeated missing safety verdicts can instead end the turn with an instruction to send a message or switch modes; detect that terminal notice next to the idle prompt without treating transient notices as waiting.
- The live bridge has no dialog event for that terminal error. Carry a separate scraper-confirmed auto-stop marker, bound to the launch generation, into daemon bridge resolution so both the waiting flag and shared client snapshots preserve it without widening ordinary scraper authority or letting a restarted session inherit a prior stop.

## Risks / Trade-offs

- Native notices may change → pin observed 2.1.325 strings in tests and keep fallback behavior owned by Claude, independent of logging.
- A manual mode switch is not evidence of startup fallback → log only explicit unavailability notices, not the generic Manual status bar.
- Removing blanket Bash adds prompts after fallback → intentional so fallback cannot skip safety review of outward shell actions.
- Classifier review can approve outward actions → recommend explicit asks only if guaranteed human approval is desired.

## Migration plan

Archive specs in the PR, pass make pre-pr, bump and record supervisor surfaces. Restart supervisor and Solo after installing. Existing sessions retain their current permissions until restarted. Revert this PR to roll back.

## Validation note

The coordinator approved an exception for this shared host's documented aggregate 300s package exhaustion: run every API/TUI top-level test exactly once in audited disjoint batches with the same race/count/timeout flags, include all nested subtests, combine coverage with passing packages and enforce the unchanged 88% floor. Record the nonzero full local gate honestly in the PR; PR CI, which uses Go's 10-minute default, is authoritative and must pass before reporting delivery complete.

## Research sources

- https://code.claude.com/docs/en/permission-modes#which-mode-a-session-starts-in
- https://code.claude.com/docs/en/permission-modes#how-auto-mode-evaluates-actions
- https://code.claude.com/docs/en/permissions#manage-permissions
- https://code.claude.com/docs/en/auto-mode-config#common-boundaries
- https://code.claude.com/docs/en/errors#the-server-returned-no-safety-verdict

## Open questions

None.
