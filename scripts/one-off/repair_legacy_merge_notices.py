#!/usr/bin/env python3
"""Retire operator-attested legacy daemon notices; never select IDs by text.

Example (dry run unless --apply is supplied):
  python3 scripts/one-off/repair_legacy_merge_notices.py --db ~/.argus/data.sql \
    --orchestrator hera-archive --role 1a-auto-archive-spec --ids 27307

Review each message first. Legacy rows have no provenance, so supplying IDs is
the operator's assertion that these are daemon prompts, not coordinator mail.
The daemon should be deployed with the typed-notice fix before applying this.
"""

import argparse
import contextlib
import datetime
import pathlib
import sqlite3


def repair(database, orchestrator, role, ids, apply=False):
    """Validate the entire explicit batch before stamping any row read."""
    if not ids or any(i <= 0 for i in ids) or len(set(ids)) != len(ids):
        raise ValueError("supply distinct positive message IDs")
    uri = pathlib.Path(database).resolve().as_uri() + "?mode=rw"
    with contextlib.closing(sqlite3.connect(uri, uri=True)) as conn, conn:
        conn.execute("BEGIN IMMEDIATE")
        target = conn.execute(
            """SELECT r.id FROM hera_roles r
            JOIN hera_orchestrators o ON o.id=r.orchestrator_id
            WHERE o.name=? AND r.name=? AND r.archived_at IS NULL""",
            (orchestrator, role),
        ).fetchall()
        if len(target) != 1:
            raise ValueError("expected exactly one active recipient role")
        role_id = target[0][0]
        bindings = conn.execute(
            """SELECT t.status FROM hera_bindings b
            JOIN tasks t ON t.id=b.argus_task_id
            WHERE b.role_id=? AND b.ended_at IS NULL""", (role_id,)
        ).fetchall()
        if bindings != [("complete",)]:
            raise ValueError("expected exactly one bound complete task")
        unread = []
        for message_id in ids:
            row = conn.execute(
                """SELECT m.to_role_id, s.kind, s.orchestrator_id=r.orchestrator_id,
                m.in_reply_to, m.read_at FROM hera_messages m
                JOIN hera_roles s ON s.id=m.from_role_id
                JOIN hera_roles r ON r.id=m.to_role_id WHERE m.id=?""",
                (message_id,),
            ).fetchone()
            if row is None or row[:4] != (role_id, "coordinator", 1, None):
                raise ValueError(f"message {message_id}: recipient/sender/reply mismatch")
            if row[4] is None:
                unread.append(message_id)
        if apply:
            now = datetime.datetime.now(datetime.timezone.utc).isoformat()
            conn.executemany(
                "UPDATE hera_messages SET read_at=? WHERE id=? AND read_at IS NULL",
                [(now, i) for i in unread],
            )
        return unread


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", required=True)
    parser.add_argument("--orchestrator", required=True)
    parser.add_argument("--role", required=True)
    parser.add_argument("--ids", required=True, nargs="+", type=int)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    try:
        ids = repair(args.db, args.orchestrator, args.role, args.ids, args.apply)
    except (ValueError, sqlite3.Error) as exc:
        parser.exit(1, f"Refused: {exc}\n")
    print(f"{'Marked read' if args.apply else 'Dry run; would mark read'}: {ids}")


if __name__ == "__main__":
    main()
