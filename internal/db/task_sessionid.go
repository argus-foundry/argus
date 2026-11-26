package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
)

// RecordResumedSession records the result of a launch without overwriting
// edits made while the runner was starting. A newer session ID wins over
// the ID the launch read before calling the runner.
func (d *DB) RecordResumedSession(id string, pid int, priorID, launchedID string) error {
	var old, current *model.Task
	err := d.WithTx(func(tx *sql.Tx) error {
		var err error
		old, err = scanTask(tx.QueryRow(`SELECT `+taskColumns+` FROM tasks WHERE id=?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
		}
		if err != nil {
			return err
		}
		row := *old
		current = &row
		current.SetStatus(model.StatusInProgress)
		current.AgentPID = pid
		if current.SessionID == priorID {
			current.SessionID = launchedID
		}
		if err := residentUpdateGuard(old, current); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE tasks SET status=?, started_at=?, ended_at=?, agent_pid=?, session_id=? WHERE id=?`,
			current.Status.String(), formatTime(current.StartedAt), formatTime(current.EndedAt),
			current.AgentPID, current.SessionID, id)
		return err
	})
	if err != nil {
		return err
	}
	if old.Status != current.Status {
		events.Emit(model.EventTypeTaskStatusChanged, id, map[string]string{
			"from": old.Status.String(), "to": current.Status.String(),
		})
	}
	events.EmitTaskUpdated(id, model.ChangedTaskFields(old, current)...)
	return nil
}

// SetSessionIDIfEmpty records a backend-minted session ID only while the task
// has none, in one atomic statement so it can never overwrite an ID another
// path (post-exit capture, resume refresh) already stored. Reports whether this
// call wrote it.
func (d *DB) SetSessionIDIfEmpty(id, sessionID string) (bool, error) {
	if id == "" || sessionID == "" {
		return false, errors.New("SetSessionIDIfEmpty: task id and session id are required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.conn.Exec(`UPDATE tasks SET session_id=? WHERE id=? AND session_id=''`, sessionID, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, d.requireTaskLocked(id, n)
}

// ReplaceSessionID overwrites a task's session ID (a Codex /new moved the
// conversation to a new thread). Reports whether the stored value changed.
func (d *DB) ReplaceSessionID(id, sessionID string) (bool, error) {
	if id == "" || sessionID == "" {
		return false, errors.New("ReplaceSessionID: task id and session id are required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.conn.Exec(`UPDATE tasks SET session_id=? WHERE id=? AND session_id<>?`, sessionID, id, sessionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, d.requireTaskLocked(id, n)
}

// RefreshSessionID replaces the ID only if the observed conversation still
// owns the row. A concurrent thread change takes precedence over recapture.
func (d *DB) RefreshSessionID(id, previous, sessionID string) (bool, error) {
	var changed bool
	err := d.WithTx(func(tx *sql.Tx) error {
		result, err := tx.Exec(`UPDATE tasks SET session_id=? WHERE id=? AND session_id=?`, sessionID, id, previous)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		changed = rows > 0
		return err
	})
	if err == nil && changed && previous != sessionID {
		events.EmitTaskUpdated(id, model.TaskFieldRuntime)
	}
	return changed, err
}

// requireTaskLocked maps a zero-row update to ErrTaskNotFound when the task is gone. Caller holds d.mu.
func (d *DB) requireTaskLocked(id string, rowsAffected int64) error {
	if rowsAffected > 0 {
		return nil
	}
	var exists int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	return nil
}
