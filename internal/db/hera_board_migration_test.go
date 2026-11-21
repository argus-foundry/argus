package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardKindsMigrationPreservesDependentsAndSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.sql")
	d, err := Open(path)
	testutil.NoError(t, err)
	c := mkRole(t, d, mkOrch(t, d, "legacy").ID, "coord", HeraKindCoordinator)
	i, err := d.PostHeraBoardItem(c.ID, "decision", "choose", "body", decisionFixture())
	testutil.NoError(t, err)
	testutil.NoError(t, d.ReadHeraBoardItem(c.ID, i.ID))
	_, err = d.conn.Exec(`INSERT INTO resident_approvals(board_id,role_id,identity,launch,created_at,state,expires_at) VALUES(?,?,'identity','launch','created','pending','expires')`, i.ID, c.ID)
	testutil.NoError(t, err)
	_, err = d.conn.Exec(`INSERT INTO hera_board_fyi_receipts(role_id,action_id,fingerprint,receipt) VALUES(?,'old','hash','{}')`, c.ID)
	testutil.NoError(t, err)
	// Install the actual pre-change table shape while keeping its children. This
	// simulates a database made by the previous binary, never a live database.
	ctx := context.Background()
	conn, err := d.conn.Conn(ctx)
	testutil.NoError(t, err)
	_, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF;
CREATE TABLE hera_board_items_old (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 role_id INTEGER NOT NULL REFERENCES hera_roles(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('decision','fyi')),
 subject TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 is_read INTEGER NOT NULL DEFAULT 0, resolved INTEGER NOT NULL DEFAULT 0,
 resolved_by TEXT NOT NULL DEFAULT '', resolution_note TEXT NOT NULL DEFAULT '',
 structured TEXT NOT NULL DEFAULT '', resolution_choice TEXT NOT NULL DEFAULT '');
INSERT INTO hera_board_items_old SELECT id,role_id,kind,subject,body,created_at,updated_at,is_read,resolved,resolved_by,resolution_note,structured,resolution_choice FROM hera_board_items;
DROP TABLE hera_board_items;
ALTER TABLE hera_board_items_old RENAME TO hera_board_items;
CREATE INDEX board_custom_index ON hera_board_items(subject);
CREATE TABLE board_audit (id INTEGER);
CREATE TRIGGER board_custom_trigger AFTER UPDATE ON hera_board_items BEGIN INSERT INTO board_audit VALUES(NEW.id); END;
UPDATE sqlite_sequence SET seq=9000 WHERE name='hera_board_items';`)
	testutil.NoError(t, err)
	testutil.NoError(t, conn.Close())
	testutil.NoError(t, d.Close())
	for n := 0; n < 2; n++ {
		d, err = Open(path)
		testutil.NoError(t, err)
		items, e := d.ListHeraBoardItems(c.ID, true)
		testutil.NoError(t, e)
		var legacy *HeraBoardItem
		for _, item := range items {
			if item.ID == i.ID {
				legacy = item
			}
		}
		testutil.NotNil(t, legacy)
		testutil.Equal(t, legacy.Read, true)
		testutil.Equal(t, legacy.UpdatedAt.Equal(i.UpdatedAt), true)
		testutil.DeepEqual(t, legacy.Structured, i.Structured)
		testutil.Equal(t, legacy.Blocking, false)
		testutil.Equal(t, legacy.Trigger, "")
		testutil.Equal(t, legacy.TriggerPending, false)
		var count, fk int
		testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM resident_approvals WHERE board_id=?`, i.ID).Scan(&count))
		testutil.Equal(t, count, 1)
		testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM hera_board_fyi_receipts WHERE action_id='old'`).Scan(&count))
		testutil.Equal(t, count, 1)
		testutil.NoError(t, d.conn.QueryRow(`PRAGMA foreign_keys`).Scan(&fk))
		testutil.Equal(t, fk, 1)
		_, e = d.conn.Exec(`INSERT INTO hera_board_items(role_id,kind,subject,body,created_at,updated_at) VALUES(-1,'todo','bad','','','')`)
		testutil.Error(t, e)
		testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('board_custom_index','board_custom_trigger')`).Scan(&count))
		testutil.Equal(t, count, 2)
		work, e := d.PostHeraBoardItemWithMetadata(c.ID, "action", "step", "body", HeraBoardMetadataPatch{Blocking: boardBool(true)})
		testutil.NoError(t, e)
		testutil.Equal(t, work.ID > 9000, true)
		testutil.NoError(t, d.ReadHeraBoardItem(c.ID, work.ID))
		testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM board_audit WHERE id=?`, work.ID).Scan(&count))
		testutil.Equal(t, count, 1)
		testutil.NoError(t, d.Close())
	}
}
