package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// migrateBoardKinds rebuilds the parent without renaming it first: SQLite
// rewrites child FK targets on rename. FK enforcement must be disabled on the
// SAME pinned connection, outside the transaction, to avoid cascading deletes.
func (d *DB) migrateBoardKinds() (result error) {
	ctx := context.Background()
	c, err := d.conn.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := c.Close(); result == nil {
			result = closeErr
		}
	}()
	var ddl string
	if err = c.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='hera_board_items'`).Scan(&ddl); err != nil {
		return err
	}
	if strings.Contains(ddl, "'action'") {
		return nil
	}
	if !strings.Contains(ddl, "'decision','fyi'") {
		return fmt.Errorf("board migration: unrecognized kind constraint")
	}
	if _, err = c.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer func() {
		_, restore := c.ExecContext(ctx, `PRAGMA foreign_keys=ON`)
		if result == nil {
			result = restore
		}
		if result != nil {
			uxlog.Log("[board] kind migration failed: %v", result)
		}
	}()
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	// Capture every explicit index/trigger before dropping the old parent.
	rows, err := tx.Query(`SELECT sql FROM sqlite_master WHERE tbl_name='hera_board_items' AND type IN ('index','trigger') AND sql IS NOT NULL`)
	if err != nil {
		return err
	}
	var dependentDDL []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			rows.Close()
			return err
		}
		dependentDDL = append(dependentDDL, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var seq int64
	if err = tx.QueryRow(`SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='hera_board_items'),0)`).Scan(&seq); err != nil {
		return err
	}
	ddl = strings.Replace(ddl, "hera_board_items", "hera_board_items_new", 1)
	ddl = strings.Replace(ddl, "'decision','fyi'", "'decision','fyi','action','todo'", 1)
	if _, err = tx.Exec(ddl); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO hera_board_items_new SELECT * FROM hera_board_items; DROP TABLE hera_board_items; ALTER TABLE hera_board_items_new RENAME TO hera_board_items;`); err != nil {
		return err
	}
	for _, s := range dependentDDL {
		if _, err = tx.Exec(s); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`UPDATE sqlite_sequence SET seq=MAX(seq,?) WHERE name='hera_board_items'`, seq); err != nil {
		return err
	}
	rows, err = tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	broken := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if broken {
		return fmt.Errorf("board migration: foreign key integrity check failed")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	uxlog.Log("[board] kind migration succeeded")
	return nil
}
