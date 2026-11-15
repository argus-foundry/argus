import contextlib
import pathlib
import sqlite3
import tempfile
import unittest

from repair_legacy_merge_notices import repair


class RepairTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = pathlib.Path(self.tmp.name) / "test.sql"
        with contextlib.closing(sqlite3.connect(self.path)) as conn, conn:
            conn.executescript("""
                CREATE TABLE hera_orchestrators(id INTEGER, name TEXT);
                CREATE TABLE hera_roles(id INTEGER, orchestrator_id INTEGER, name TEXT,
                    kind TEXT, archived_at TEXT);
                CREATE TABLE tasks(id TEXT, status TEXT);
                CREATE TABLE hera_bindings(role_id INTEGER, argus_task_id TEXT, ended_at TEXT);
                CREATE TABLE hera_messages(id INTEGER, from_role_id INTEGER, to_role_id INTEGER,
                    in_reply_to INTEGER, read_at TEXT);
                INSERT INTO hera_orchestrators VALUES(1,'o');
                INSERT INTO hera_roles VALUES(1,1,'coord','coordinator',NULL),
                    (2,1,'worker','worker',NULL),(3,1,'other','worker',NULL);
                INSERT INTO tasks VALUES('t','complete');
                INSERT INTO hera_bindings VALUES(2,'t',NULL);
                INSERT INTO hera_messages VALUES(7,1,2,NULL,NULL),(8,1,2,NULL,NULL),
                    (9,1,3,NULL,NULL),(10,3,2,NULL,NULL),(11,1,2,7,NULL);
            """)

    def unread(self):
        with contextlib.closing(sqlite3.connect(self.path)) as conn, conn:
            return conn.execute("SELECT id FROM hera_messages WHERE read_at IS NULL ORDER BY id").fetchall()

    def test_dry_run_and_explicit_batch_only_and_idempotence(self):
        before = self.unread()
        self.assertEqual(repair(self.path, "o", "worker", [7, 8]), [7, 8])
        self.assertEqual(self.unread(), before)
        self.assertEqual(repair(self.path, "o", "worker", [7, 8], True), [7, 8])
        self.assertEqual(self.unread(), [(9,), (10,), (11,)])
        self.assertEqual(repair(self.path, "o", "worker", [7, 8], True), [])

    def test_batch_mismatch_writes_nothing(self):
        for bad_id in [9, 10, 11, 999]:
            with self.subTest(bad_id=bad_id):
                before = self.unread()
                with self.assertRaises(ValueError):
                    repair(self.path, "o", "worker", [7, bad_id], True)
                self.assertEqual(self.unread(), before)

    def test_wrong_recipient_or_incomplete_task_refuses(self):
        with self.assertRaises(ValueError):
            repair(self.path, "o", "missing", [7], True)
        with contextlib.closing(sqlite3.connect(self.path)) as conn, conn:
            conn.execute("UPDATE tasks SET status='in_review'")
        with self.assertRaises(ValueError):
            repair(self.path, "o", "worker", [7], True)
        self.assertIn((7,), self.unread())

    def test_invalid_ids_refuse(self):
        for ids in [[], [7, 7], [-1]]:
            with self.subTest(ids=ids), self.assertRaises(ValueError):
                repair(self.path, "o", "worker", ids, True)


if __name__ == "__main__":
    unittest.main()
