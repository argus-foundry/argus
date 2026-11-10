package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
)

// SetTaskAccountMove persists the selected account and its conversation together.
func (d *DB) SetTaskAccountMove(id, account string, routed bool, sessionID string) error {
	accountChanged, sessionChanged, err := d.setTaskAccountLocked(id, account, routed, &sessionID)
	if err != nil {
		return err
	}
	var fields []string
	if accountChanged {
		fields = append(fields, model.TaskFieldAccount)
	}
	if sessionChanged {
		fields = append(fields, model.TaskFieldRuntime)
	}
	if len(fields) > 0 {
		events.EmitTaskUpdated(id, fields...)
	}
	return nil
}

// SetTaskAccount writes only a task's account and routed flag, so an account
// move never reverts a concurrent edit to any other column.
func (d *DB) SetTaskAccount(id, account string, routed bool) error {
	changed, _, err := d.setTaskAccountLocked(id, account, routed, nil)
	if err != nil {
		return err
	}
	if changed {
		events.EmitTaskUpdated(id, model.TaskFieldAccount)
	}
	return nil
}

func (d *DB) setTaskAccountLocked(id, account string, routed bool, sessionID *string) (bool, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if gerr := residentGuard(d.conn, id); gerr != nil {
		return false, false, gerr
	}
	var oldAccount, oldSessionID string
	var oldRouted int
	if err := d.conn.QueryRow(`SELECT account, account_routed, session_id FROM tasks WHERE id=?`, id).Scan(&oldAccount, &oldRouted, &oldSessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
		}
		return false, false, err
	}
	newSessionID := oldSessionID
	if sessionID != nil {
		newSessionID = *sessionID
	}
	if _, err := d.conn.Exec(`UPDATE tasks SET account=?, account_routed=?, session_id=? WHERE id=?`, account, boolToInt(routed), newSessionID, id); err != nil {
		return false, false, err
	}
	return oldAccount != account || (oldRouted != 0) != routed, oldSessionID != newSessionID, nil
}
