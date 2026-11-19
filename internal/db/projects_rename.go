package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrProjectExists = errors.New("project already exists")

// RenameProject rewrites every reference to a project name in one transaction.
func (d *DB) RenameProject(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("project name must not be empty")
	}
	if strings.Contains(newName, "/") {
		return errors.New("project name must not contain '/'")
	}
	if newName == oldName {
		return errors.New("new project name must differ from the old one")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM projects WHERE name = ?`, oldName).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrProjectNotFound
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM projects WHERE name = ?`, newName).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrProjectExists
	}
	for _, q := range []string{
		`UPDATE projects SET name = ? WHERE name = ?`,
		`UPDATE tasks SET project = ? WHERE project = ?`,
		`UPDATE scheduled_tasks SET project = ? WHERE project = ?`,
		`UPDATE kb_pending_tasks SET project = ? WHERE project = ?`,
		`UPDATE sandbox_proposals SET project = ? WHERE project = ?`,
		`UPDATE hera_roles SET argus_project = ? WHERE argus_project = ?`,
	} {
		if _, err := tx.Exec(q, newName, oldName); err != nil {
			return fmt.Errorf("renaming project: %w", err)
		}
	}

	var raw string
	err = tx.QueryRow(`SELECT value FROM config WHERE key = ?`, ConfigKeyProjectAccounts).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if raw != "" {
		m := map[string]string{}
		if json.Unmarshal([]byte(raw), &m) == nil {
			if acct, ok := m[oldName]; ok {
				delete(m, oldName)
				m[newName] = acct
				b, err := json.Marshal(m)
				if err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE config SET value = ? WHERE key = ?`, string(b), ConfigKeyProjectAccounts); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}
