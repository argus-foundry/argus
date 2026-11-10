package db

import "fmt"

// TaskAccountReferences includes resident and archived rows without loading task history.
func (d *DB) TaskAccountReferences(name string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT id, account FROM tasks ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query task account references: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id, account string
		if err := rows.Scan(&id, &account); err != nil {
			return nil, fmt.Errorf("scan task account reference: %w", err)
		}
		if account == name {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task account references: %w", err)
	}
	return ids, nil
}
