package db

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
)

// SetManualPR records url as the task's manual PR association and invalidates
// the derived pr cache so nothing from a previous association survives.
func (d *DB) SetManualPR(id, url string) error {
	err := d.WithTx(func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM tasks WHERE id=?`, id).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
			}
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO task_pr_links (task_id, url, created_at) VALUES (?, ?, ?)
			 ON CONFLICT(task_id) DO UPDATE SET url=excluded.url, created_at=excluded.created_at`,
			id, url, formatTime(time.Now())); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM task_meta WHERE task_id=? AND namespace=?`, id, prMetaNamespace)
		return err
	})
	if err == nil {
		events.EmitTaskUpdated(id, model.TaskFieldPR)
	}
	return err
}

// ClearManualPR drops the manual association and the pr cache derived from it,
// returning automatic discovery to the branch poller.
func (d *DB) ClearManualPR(id string) error {
	err := d.WithTx(func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM tasks WHERE id=?`, id).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
			}
			return err
		}
		if _, err := tx.Exec(`DELETE FROM task_pr_links WHERE task_id=?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM task_meta WHERE task_id=? AND namespace=?`, id, prMetaNamespace)
		return err
	})
	if err == nil {
		events.EmitTaskUpdated(id, model.TaskFieldPR)
	}
	return err
}

// ManualPR returns the task's manually linked PR url, or "" when none.
func (d *DB) ManualPR(id string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var url string
	err := d.conn.QueryRow(`SELECT url FROM task_pr_links WHERE task_id=?`, id).Scan(&url)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return url, err
}

// ManualPRs returns every manual association keyed by task id.
func (d *DB) ManualPRs() (map[string]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT task_id, url FROM task_pr_links`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	out := map[string]string{}
	for rows.Next() {
		var id, url string
		if err := rows.Scan(&id, &url); err != nil {
			return nil, err
		}
		out[id] = url
	}
	return out, rows.Err()
}

// SetAutoPRMetaIfChanged is SetMetaBatchIfChanged for the pr namespace, but
// refuses (false, nil) when the task has a manual association so branch
// polling can never replace manual intent, even for a poll already in flight.
func (d *DB) SetAutoPRMetaIfChanged(id string, entries map[string]string) (bool, error) {
	return d.setPRMetaIfChanged(id, "", entries)
}

// SetManualPRMetaIfChanged writes the verified state of the manual PR url,
// but only while url is still the task's association.
func (d *DB) SetManualPRMetaIfChanged(id, url string, entries map[string]string) (bool, error) {
	return d.setPRMetaIfChanged(id, url, entries)
}

// setPRMetaIfChanged writes entries under the pr namespace when the task's
// manual link equals wantLink ("" means no link).
func (d *DB) setPRMetaIfChanged(id, wantLink string, entries map[string]string) (bool, error) {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	now := formatTime(time.Now())
	wrote := false
	err := d.WithTx(func(tx *sql.Tx) error {
		var link string
		if err := tx.QueryRow(`SELECT url FROM task_pr_links WHERE task_id=?`, id).Scan(&link); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if link != wantLink {
			return nil
		}
		changed := false
		for _, k := range keys {
			var cur string
			err := tx.QueryRow(`SELECT value FROM task_meta WHERE task_id=? AND namespace=? AND key=?`, id, prMetaNamespace, k).Scan(&cur)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && cur != entries[k]) {
				changed = true
				break
			}
			if err != nil {
				return err
			}
		}
		if !changed {
			return nil
		}
		for _, k := range keys {
			if _, err := tx.Exec(
				`INSERT INTO task_meta (task_id, namespace, key, value, updated_at) VALUES (?, ?, ?, ?, ?)
				 ON CONFLICT(task_id, namespace, key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
				id, prMetaNamespace, k, entries[k], now); err != nil {
				return err
			}
		}
		wrote = true
		return nil
	})
	if wrote && err == nil {
		events.EmitTaskUpdated(id, model.TaskFieldPR)
	}
	return wrote && err == nil, err
}
