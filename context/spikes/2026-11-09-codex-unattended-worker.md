# Codex unattended-worker spike

- **Scope:** approved `add-resident-coordinator`, task 4.0; documentation only, no production flags or eligibility changed.
- **Date/version:** 2026-11-09, `codex-cli 0.167.0`, macOS, real interactive PTY in the Argus worker worktree.
- **Verdict:** no verified configuration removes all question paths on launch and resume; retain the fail-closed Codex exclusion specified by design D4. This is a version-scoped eligibility decision, not a claim that every possible future CLI configuration is impossible.
- **Session:** `01a1c279-c36c-7f33-ac38-0d6f5bf9b812`, model `gpt-6-astra`; excerpts below omit account identifiers, credentials, inherited instructions and unrelated tool metadata.

## Launch fidelity and environment

Read `internal/agent/backends.go` (`codex` family, `codexResumeCommand`) and `internal/agent/agent.go` (`codexResumeFlags`). Argus's default is `codex --dangerously-bypass-approvals-and-sandbox`; resume reconstructs the command as `codex … resume --dangerously-bypass-approvals-and-sandbox … <session-id>`, so flags baked only into the original command are not automatically preserved by Argus.

The first PTY command was the default bypass plus `--no-alt-screen` and this harmless prompt:

```text
This is an unattended-input capability probe. Do not read files, run commands,
or modify anything. Invoke request_user_input with a required choice between
red and blue, and wait for the answer. If the tool is unavailable, state exactly
that and finish.
```

It exited before a model turn:

```text
Error: failed to create daemon state directory
/Users/aaron/.local/share/argus/codex-home/app-server-daemon:
socket directory path exists and is not a directory: <same path>
To work without the background server, rerun the same command with --no-daemon
(including resume or fork and its arguments).
```

That overlay path is a symlink to `/Users/aaron/.codex/app-server-daemon`. The error alone does not establish an Argus sandbox denial. Adding the CLI-suggested `--no-daemon` allowed real fresh and resumed turns; this spike therefore does **not** rely solely on the debug fallback. `--no-alt-screen` affects presentation only. No shared daemon was stopped or reconfigured.

## Observed prompts and candidate closures

| Path | Evidence / candidate | Result |
| --- | --- | --- |
| Command approvals / escalation | Existing bypass; rollout and debug permissions both say `never`. `-a never` is the explicit approval-policy spelling in help. | Closed by policy for command approvals; debug instructions explicitly reject `sandbox_permissions` requests. Not a question-tool denylist. No destructive commands or external approval requests were issued. |
| Default synchronous question | Fresh probe requested `request_user_input`. | Model answered `request_user_input is unavailable.` Default-mode restriction alone does not cover the async tool. |
| Default async question | Explicitly requested `functions.request_user_input_async` after the fresh probe. | Accepted; displayed red/blue question; tool returned immediately and model ended its turn with question unanswered. This is not a synchronously blocked tool, but the question channel remains available. |
| Candidate feature overrides | Resume with `features.collaboration_modes=false` and `features.default_mode_request_user_input=false`. | Async question accepted again. `codex features list` labels collaboration modes `removed`; even when displayed as false, `/plan` still works. |
| Plan synchronous question | In that resumed process, manually selected `/plan`, then explicitly requested `functions.request_user_input`. | Real call recorded, no tool result or completed turn while unanswered for more than 60 seconds; Ctrl+C interrupted it. Manual mode selection is part of this probe, not evidence that the model can switch its own mode. |
| Direct feature removal guesses | `--disable request_user_input`; separately `--disable request_user_input_async`. | Both rejected as unknown feature flags. |
| Direct config removal guesses | Strict startup validation of `tools.request_user_input=false`; separately `tools.request_user_input_async=false`. | Both rejected as unknown configuration fields. A non-strict accepted `-c` alone is not proof that an override has an effect. |
| MCP/app elicitation, auth, hook trust, setup and ordinary free-text questions | Official configuration reference describes granular elicitation/permission/skill approval policy; local help separately offers `--dangerously-bypass-hook-trust`. | Not exhaustively exercised. None of these establish removal of built-in question tools. No additional switch is certified by this spike; orientation and needs-input notifications remain backstops, not mechanical tool removal. |

## Fresh and resumed transcript

Fresh PTY launch (same prompt as above):

```sh
codex --no-daemon --dangerously-bypass-approvals-and-sandbox --no-alt-screen '<probe prompt>'
```

At 22:42:04 UTC, rollout turn context recorded `approval_policy: "never"`, `collaboration_mode.mode: "default"`; model final: `request_user_input is unavailable.` A second turn explicitly asked for the async tool:

```text
22:42:57.391Z function_call request_user_input_async
  {"questions":[{"title":"Choose red or blue.","options":["Red","Blue"]}]}
22:42:57.400Z function_call_output
  {"accepted":true}
UI: ? Choose red or blue. / Red / Blue
Model final: Asked you to choose red or blue.
```

Quit this probe via `/quit`, then resumed the exact session, not `--last`:

```sh
codex resume --no-daemon --no-alt-screen --dangerously-bypass-approvals-and-sandbox \
  -c 'features.collaboration_modes=false' \
  -c 'features.default_mode_request_user_input=false' \
  01a1c279-c36c-7f33-ac38-0d6f5bf9b812 \
  'Unattended-input probe after resume: invoke functions.request_user_input_async with a red-or-blue question now. Do not use any other tools. If unavailable, state that and finish.'
```

```text
22:43:31.837Z turn_context: approval_policy=never, collaboration_mode=default
22:43:34.458Z function_call request_user_input_async
  {"questions":[{"title":"Choose red or blue.","options":["Red","Blue"]}]}
22:43:34.484Z function_call_output {"accepted":true}
UI: ? Choose red or blue. / Red / Blue
Model final: Asked you to choose red or blue.
```

Then typed `/plan`; the CLI status changed to `Plan mode` despite the false collaboration feature. Sent:

```text
Invoke functions.request_user_input with one required question: Choose red or
blue, options Red and Blue. Do not use request_user_input_async or any other
tool. This is a blocking-input probe in Plan mode.
```

```text
22:44:13.002Z turn_context: approval_policy=never, collaboration_mode=plan
22:44:17.118Z function_call request_user_input
  {"questions":[{"header":"Color","id":"color_choice",
    "question":"Choose red or blue.","options":[
      {"label":"Red","description":"Choose red."},
      {"label":"Blue","description":"Choose blue."}]}]}
```

The emitted call's `status: "completed"` marks the generated function-call item, not a returned tool result: there was no matching `function_call_output`. The rollout still ended at that call/token-usage record more than 60 seconds later. Ctrl+C produced `Conversation interrupted`; no choice was answered. The probe was then quit. This demonstrates an available blocking path in a resumed process, not an indefinite-wait proof or proof of mode persistence across another resume.

## Debug and CLI evidence

Following the `add-nonclaude-context-parity` precedent, additionally rendered the actual model input:

```sh
codex debug prompt-input \
  -c 'approval_policy="never"' -c 'sandbox_mode="danger-full-access"' \
  -c 'features.default_mode_request_user_input=false' \
  -c 'features.collaboration_modes=false' UNATTENDED_SPIKE_PROMPT
```

The rendered permissions state `Approval policy is currently never` and `Do not provide the sandbox_permissions … commands will be rejected.` The `additional_tools` functions namespace **still lists both** `request_user_input` and `request_user_input_async`. Default-mode instructions restrict when the synchronous tool may be used; instructions are not tool removal. Debug rendering is supplementary; the real turns above establish behavior.

```text
codex --disable collaboration_modes features list
  collaboration_modes                removed             false
  default_mode_request_user_input    under development   false

codex --disable request_user_input features list
  Error: Unknown feature flag: request_user_input
codex --disable request_user_input_async features list
  Error: Unknown feature flag: request_user_input_async

codex --strict-config -c 'tools.request_user_input=false' app-server --print-startup-config
  unknown configuration field `tools.request_user_input` in -c/--config override
codex --strict-config -c 'tools.request_user_input_async=false' app-server --print-startup-config
  unknown configuration field `tools.request_user_input_async` in -c/--config override
```

## Sources and implementation handoff

- Local `codex --help`, `codex resume --help`, `codex debug prompt-input --help`, `codex features list`, strict startup validation and the real probe's rollout supplied the version-specific evidence. `resume --help` accepts `-c`, `--disable`, `-a never` and bypass; accepting a flag does not prove it closes a question path.
- [Official CLI reference](https://developers.openai.com/codex/cli/reference/) and [official configuration reference](https://developers.openai.com/codex/config-reference/) were fetched on 2026-11-09 (redirecting to ChatGPT Learn). The configuration reference describes `approval_policy` and granular `mcp_elicitations`, `request_permissions`, `sandbox_approval`, `rules`, `skill_approval`; the fetched reference does not document a built-in question-tool denylist.
- **Flags to apply for Codex resident workers: none.** Retain `Family.UnattendedWorker=nil` and filter/reject Codex for these workers. Existing bypass is useful for command approvals but insufficient for eligibility. Do not ship the false feature overrides as a safety mechanism. Re-run this spike when a supported question-tool removal mechanism becomes available.
- **Task implications:** task 4.0 is complete; skip Codex implementation 4.3. The coordinator must reconcile task 4.0's literal "4.3/4.6 are dropped" with design D4's Claude-eligible account setting; this documentation-only spike does not edit those approved artifacts.

## Validation

- Real fresh/resume PTY probes, debug prompt rendering and strict config rejection probes completed; both interactive probe processes exited via `/quit`.
- `git diff --cached --check` passed; `openspec validate add-resident-coordinator --strict` passed.
- `openspec validate --all --strict`: 80 passed, 1 failed. The unchanged `desktop-web-workspace` base spec has two existing requirements missing SHALL/MUST (`requirements.5.text`, `requirements.20.text`); no spec files changed in this spike.
- Full `make pre-pr` via iris run `8ac209c8230c26c3d76ce77f6c7f589f`: build, vet, formatting, lint (0 issues) and vulnerability checks passed. Race coverage stopped at 300-second timeouts in `internal/agent`, `internal/api`, `internal/tui`; agent rerender shared-daemon/detached-tree tests also hit process-inspection deadlines. Host load measured immediately afterward was `119.53 117.23 68.07`, matching the documented contention failure shape in `gotchas/ci-gates.md`; causation was not independently proven by an isolated rerun. Coverage floor and workspace tests were not reached. No production/test code was changed to work around these failures, and no PR was opened by this worker.

CODEX_INELIGIBLE: Codex 0.167.0 retains question tools under bypass and tested feature overrides, including after resume; no verified launch/resume closure.
