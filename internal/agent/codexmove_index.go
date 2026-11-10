package agent

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

func openCodexDB(path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func codexThreadSchema(conn *sql.DB) (map[string]bool, error) {
	rows, err := conn.Query(`PRAGMA table_info(threads)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("no threads table")
	}
	return cols, nil
}

func sharedThreadColumns(have map[string]bool) []string {
	var out []string
	for _, c := range codexThreadColumns {
		if have[c] {
			out = append(out, c)
		}
	}
	return out
}

// readCodexThread returns the allowlisted columns of thread id, or nil when
// the index has no such thread.
func readCodexThread(dbPath, id string) (map[string]any, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	conn, err := openCodexDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	have, err := codexThreadSchema(conn)
	if err != nil {
		return nil, err
	}
	cols := sharedThreadColumns(have)
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	err = conn.QueryRow(`SELECT `+strings.Join(cols, ",")+` FROM threads WHERE id = ?`, id).Scan(ptrs...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row := make(map[string]any, len(cols))
	for i, c := range cols {
		row[c] = vals[i]
	}
	return row, nil
}

// upsertCodexThread writes row into the target index, keeping any columns the
// target lacks out of the statement.
func upsertCodexThread(dbPath string, row map[string]any) error {
	conn, err := openCodexDB(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	have, err := codexThreadSchema(conn)
	if err != nil {
		return err
	}
	var cols, marks, sets []string
	var args []any
	for _, c := range sharedThreadColumns(have) {
		v, ok := row[c]
		if !ok {
			continue
		}
		cols = append(cols, c)
		marks = append(marks, "?")
		args = append(args, v)
		if c != "id" {
			sets = append(sets, c+"=excluded."+c)
		}
	}
	//nolint:gosec // identifiers come from the fixed codexThreadColumns allowlist
	q := `INSERT INTO threads(` + strings.Join(cols, ",") + `) VALUES(` + strings.Join(marks, ",") + `)` +
		` ON CONFLICT(id) DO UPDATE SET ` + strings.Join(sets, ",")
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(q, args...); err != nil {
		return err
	}
	return tx.Commit()
}

func validateCodexMoveTarget(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("state database is not a regular file")
	}
	conn, err := openCodexDB(path)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	cols, err := codexThreadSchema(conn)
	if err != nil {
		return err
	}
	for _, name := range []string{"id", "rollout_path", "cwd", "source", "updated_at"} {
		if !cols[name] {
			return fmt.Errorf("threads is missing %s", name)
		}
	}
	return nil
}
