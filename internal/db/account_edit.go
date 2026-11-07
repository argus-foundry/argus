package db

import (
	"context"
	"fmt"

	"github.com/argus-foundry/argus/internal/config"
)

// AccountValidationError preserves the validation message for callers.
type AccountValidationError struct{ Err error }

func (e *AccountValidationError) Error() string { return e.Err.Error() }
func (e *AccountValidationError) Unwrap() error { return e.Err }

// UpdateAccountFields validates the merged row under SQLite's write lock.
func (d *DB) UpdateAccountFields(name string, fields map[string]string) error {
	if len(fields) == 0 {
		return fmt.Errorf("no account fields supplied")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ctx := context.Background()
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	// A deferred transaction could read a stale row before another process writes.
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(ctx, `ROLLBACK`) }()
	var merged config.Account
	if err := conn.QueryRowContext(ctx, `SELECT label, claude_config_dir, codex_home FROM accounts WHERE name = ?`, name).Scan(&merged.Label, &merged.ClaudeConfigDir, &merged.CodexHome); err != nil {
		return err
	}
	for field, value := range fields {
		switch field {
		case "label":
			merged.Label = value
		case "claude_config_dir":
			merged.ClaudeConfigDir = value
		case "codex_home":
			merged.CodexHome = value
		default:
			return fmt.Errorf("unknown account field %q", field)
		}
	}
	if err := (config.Config{Accounts: map[string]config.Account{name: merged}}).ValidateAccount(name); err != nil {
		return &AccountValidationError{Err: err}
	}
	if _, err := conn.ExecContext(ctx, `UPDATE accounts SET label = ?, claude_config_dir = ?, codex_home = ? WHERE name = ?`, merged.Label, merged.ClaudeConfigDir, merged.CodexHome, name); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `COMMIT`)
	return err
}
