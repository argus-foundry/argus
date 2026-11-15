#!/usr/bin/env python3
"""Harmless installed-Claude proof; inherits CLAUDE_CONFIG_DIR, never reads auth.

Run: python3 context/spikes/resident-defer-proof.py
Requires claude, expect, Python 3. PROOF_BASE must be inside a folder Claude
already trusts: on 2.1.325 the expect trust answer does not dismiss startup trust.
Set PROOF_FAILURES_ONLY=1 for crash/timeout cases only. Set PROOF_BASE to a trusted directory and
PROOF_MODE=auto to repeat in auto mode. Hook/settings/output files are temporary;
interactive transcripts are retained by Claude (the CLI's no-persistence flag
is print-only). Never reads or prints credentials.
"""

import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import time


def hook():
    root, decision = Path(sys.argv[2]), sys.argv[3]
    event = json.load(sys.stdin)
    # Capture only proof metadata, never environment or credential material.
    record = {key: event.get(key) for key in (
        "hook_event_name", "tool_name", "tool_input", "transcript_path", "session_id"
    )}
    with (root / "events.jsonl").open("a") as stream:
        stream.write(json.dumps(record) + "\n")
    if event["hook_event_name"] == "PermissionRequest":
        if decision == "crash":
            sys.exit(1)
        if decision == "timeout":
            time.sleep(20)
            return
        answer = {"behavior": decision}
        if decision == "deny":
            answer.update(message="Queued for Aaron as #1. Do not retry; continue with your final response.", interrupt=False)
        print(json.dumps({"hookSpecificOutput": {
            "hookEventName": "PermissionRequest", "decision": answer
        }}))
    else:
        print("{}")


EXPECT = r'''
set timeout 85
log_user 0
log_file -a -noappend $env(ARGUS_PROOF_OUTPUT)
spawn -noecho {*}$argv
set trusted 0
expect {
    -re {Do.{0,12}you.{0,12}want.{0,12}to.{0,12}proceed} {
        set ::env(PROOF_DIALOG) 1
        set f [open "$::env(ARGUS_PROOF_OUTPUT).dialog" a]; puts $f seen; close $f
        expect {
            -timeout 15
            -re {5555ok} { after 1000; send -- "\003"; after 300; send -- "\003"; catch {close}; catch {wait}; exit 0 }
            timeout { send -- "\003"; catch {close}; catch {wait}; exit 4 }
            eof { catch {wait}; exit 2 }
        }
    }
    -re {5555ok} {
        after 1000
        send -- "\003"
        after 300
        send -- "\003"
        after 300
        catch {close}
        catch {wait}
        exit 0
    }
    -re {trust.{0,40}folder} {
        if {$trusted == 0} { set trusted 1; send -- "2"; after 500; send -- "\r" }
        exp_continue
    }
    -re {Choose the text style} { send -- "\r"; exp_continue }
    timeout { catch {close}; catch {wait}; exit 3 }
    eof { catch {wait}; exit 2 }
}
'''


def run_case(base, label, interactive, decision, permission_mode=os.environ.get("PROOF_MODE","default")):
    root = base / label
    root.mkdir()
    hook_command = shlex.join([sys.executable, str(Path(__file__).resolve()), "--hook", str(root), decision])
    handler = {"type": "command", "command": hook_command, "onFailure": "block", "timeout": 2 if decision == "timeout" else 10}
    settings = {
        "permissions": {"ask": ["Bash"]},
        "hooks": {name: [{"matcher": "Bash", "hooks": [handler]}]
                  for name in ("PermissionRequest", "PostToolUse")},
    }
    settings_path = root / "settings.json"
    settings_path.write_text(json.dumps(settings))
    prompt = (
        "This is a harmless permission-hook integration test. Call Bash exactly once "
        "with command echo argus-probe and no other commands. After its result, including "
        "a denial, continue and finish by writing the sum of 1234 and 4321 immediately "
        "followed by the lowercase letters ok, with no space. Do not retry denied calls."
    )
    args = ["claude", "--model", os.environ.get("ARGUS_PROOF_MODEL", "haiku"),
            "--permission-mode", permission_mode, "--setting-sources", "",
            "--settings", str(settings_path), "--tools", "Bash",
            "--strict-mcp-config", "--mcp-config", '{"mcpServers":{}}',
            "--disable-slash-commands", "--no-chrome"]
    if not interactive:
        args.append("--no-session-persistence")
    env = dict(os.environ)
    env["TERM"] = "xterm-256color"
    # This child is an isolated test session, not a nested agent or Argus task.
    for key in ("CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "ARGUS_TASK_ID", "ARGUS_RESIDENT"):
        env.pop(key, None)
    output_path = root / "output.txt"
    try:
        if interactive:
            driver = root / "probe.exp"
            driver.write_text(EXPECT)
            env["ARGUS_PROOF_OUTPUT"] = str(output_path)
            result = subprocess.run(["expect", str(driver), *args, prompt], cwd=root,
                                    env=env, capture_output=True, text=True, timeout=100)
            output = output_path.read_text(errors="replace") if output_path.exists() else ""
            output += result.stdout + result.stderr
        else:
            result = subprocess.run([*args, "-p", "--max-turns", "4", "--output-format", "json", prompt],
                                    cwd=root, env=env, capture_output=True, text=True, timeout=100)
            output = result.stdout + result.stderr
            output_path.write_text(output)
    except subprocess.TimeoutExpired:
        print(f"FAIL {label}: subprocess timeout", flush=True)
        return False
    events_path = root / "events.jsonl"
    events = [json.loads(line) for line in events_path.read_text().splitlines()] if events_path.exists() else []
    asks = [e for e in events if e["hook_event_name"] == "PermissionRequest"]
    executed = [e for e in events if e["hook_event_name"] == "PostToolUse"]
    expected_command = lambda e: e["tool_name"] == "Bash" and e["tool_input"].get("command") == "echo argus-probe"
    correct_calls = all(expected_command(e) for e in asks + executed)
    import re as _re
    continued = bool(_re.search(r"5555ok", output))
    failure_case = decision in ("crash", "timeout")
    ok = (len(asks) == 1 and correct_calls and len(executed) == (1 if decision == "allow" else 0)
          and (failure_case or (result.returncode == 0 and continued)))
    # --no-session-persistence is documented for -p only. Verify the interactive
    # launch didn't persist too; never remove or alter an existing transcript.
    persisted = [e["transcript_path"] for e in events if e.get("transcript_path")
                 and Path(e["transcript_path"]).exists()]
    dialog = Path(str(output_path)+".dialog").exists()
    outcome = "allow" if executed else "native-dialog" if dialog and not continued else "blocked"
    print(f"{'PASS' if ok else 'FAIL'} {label}: outcome={outcome} dialog_seen={dialog} exit={result.returncode} permission_requests={len(asks)} "
          f"executions={len(executed)} continued={continued} persisted={bool(persisted)}", flush=True)
    if persisted:
        print(f"NOTE {label}: interactive proof transcript retained by Claude (print-only no-persistence flag)", flush=True)
    if not ok:
        # Do not emit raw session output: a startup failure could contain account
        # data. Keep it local; report only known diagnostic categories.
        for needle in ("usage_limit_reached", "weekly limit", "not logged in", "Unknown option",
                       "only works with", "Invalid settings", "Do you want to proceed?"):
            if needle.lower() in output.lower():
                print(f"  diagnostic: {needle}", flush=True)
    return ok


def main():
    if not os.environ.get("CLAUDE_CONFIG_DIR"):
        print("FAIL preflight: CLAUDE_CONFIG_DIR must name your already authenticated account")
        return 1
    for binary in ("claude", "expect"):
        if not shutil.which(binary):
            print(f"FAIL preflight: {binary} unavailable")
            return 1
    print(subprocess.check_output(["claude", "--version"], text=True).strip(), flush=True)
    for directory in [tempfile.mkdtemp(prefix="argus-defer-proof-", dir=os.environ.get("PROOF_BASE"))]:
        print("dir", directory, flush=True)
        base = Path(directory)
        results = [] if os.environ.get("PROOF_FAILURES_ONLY") else [
            run_case(base, "print-ask-allow", False, "allow"),
            run_case(base, "pty-ask-allow", True, "allow"),
            run_case(base, "print-deny-continue", False, "deny"),
            run_case(base, "pty-deny-continue", True, "deny"),
        ]
        results.extend(run_case(base, f"{transport}-{decision}", interactive, decision)
                       for decision in ("crash", "timeout")
                       for transport, interactive in (("print", False), ("pty", True)))
    print("PASS all cases" if all(results) else "FAIL one or more cases", flush=True)
    return 0 if all(results) else 1


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--hook":
        hook()
    else:
        sys.exit(main())
