package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
)

// RecordResumedSession records the result of a launch without overwriting
// edits made while the runner was starting. A newer session ID wins over
// the ID the launch read before calling the runner.
func (d *DB) RecordResumedSession(id string, pid int, priorID, launchedID string) error {
	_, err := d.recordSessionLaunch(id, pid, priorID, launchedID, false, nil)
	return err
}

// RecordResumedSessionIfUnstopped returns the persisted row, or nil if a
// successful stop has superseded this launch even when the task status did
// not change. Capture stopGeneration before calling the runner.
func (d *DB) RecordResumedSessionIfUnstopped(id string, pid int, priorID, launchedID string, stopGeneration int64) (*model.Task, error) {
	return d.recordSessionLaunch(id, pid, priorID, launchedID, false, &stopGeneration)
}

// RecordFailedResumeIfUnstopped rolls back a failed TUI launch while
// preserving later stops and any session ID recorded by another writer.
func (d *DB) RecordFailedResumeIfUnstopped(id, generatedID string, stopGeneration int64) (*model.Task, error) {
	return d.recordSessionChange(id, &stopGeneration, func(task *model.Task) bool {
		task.SetStatus(model.StatusPending)
		task.StartedAt = time.Time{}
		if generatedID != "" && task.SessionID == generatedID {
			task.SessionID = ""
		}
		return true
	})
}

// RecordInitialSession promotes a Pending row when its first session starts.
// If a stop or another lifecycle action has already advanced the row, it
// returns that state instead of overwriting it.
func (d *DB) RecordInitialSession(id string, pid int, priorID, launchedID string) (*model.Task, error) {
	return d.recordSessionLaunch(id, pid, priorID, launchedID, true, nil)
}

func (d *DB) recordSessionLaunch(id string, pid int, priorID, launchedID string, initial bool, stopGeneration *int64) (*model.Task, error) {
	return d.recordSessionChange(id, stopGeneration, func(task *model.Task) bool {
		if initial && task.Status != model.StatusPending {
			return false
		}
		task.SetStatus(model.StatusInProgress)
		task.AgentPID = pid
		if task.SessionID == priorID {
			task.SessionID = launchedID
		}
		return true
	})
}

func (d *DB) recordSessionChange(id string, stopGeneration *int64, change func(*model.Task) bool) (*model.Task, error) {
	var old, current *model.Task
	err := d.WithTx(func(tx *sql.Tx) error {
		// Acquire write intent before reading the task. With WAL, upgrading
		// a read transaction can fail immediately if another handle writes.
		generation, err := scanStopGeneration(id, tx.QueryRow(`UPDATE tasks SET stop_generation=stop_generation WHERE id=? RETURNING stop_generation`, id))
		if err != nil {
			return err
		}
		if stopGeneration != nil && generation != *stopGeneration {
			return nil
		}
		old, err = scanTask(tx.QueryRow(`SELECT `+taskColumns+` FROM tasks WHERE id=?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
		}
		if err != nil {
			return err
		}
		row := *old
		if !change(&row) {
			current = old
			return nil
		}
		current = &row
		if err := residentUpdateGuard(old, current); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE tasks SET status=?, started_at=?, ended_at=?, agent_pid=?, session_id=? WHERE id=?`,
			current.Status.String(), formatTime(current.StartedAt), formatTime(current.EndedAt),
			current.AgentPID, current.SessionID, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	if old == current {
		return current, nil
	}
	if old.Status != current.Status {
		events.Emit(model.EventTypeTaskStatusChanged, id, map[string]string{
			"from": old.Status.String(), "to": current.Status.String(),
		})
	}
	events.EmitTaskUpdated(id, model.ChangedTaskFields(old, current)...)
	return current, nil
}

// SessionStopGeneration returns the stop counter for a task. Read it before
// launch to distinguish a subsequent stop from an unchanged InReview status.
func (d *DB) SessionStopGeneration(id string) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return scanStopGeneration(id, d.conn.QueryRow(`SELECT stop_generation FROM tasks WHERE id=?`, id))
}

func scanStopGeneration(id string, row *sql.Row) (int64, error) {
	var generation int64
	err := row.Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	return generation, err
}

// RecordSessionStop records a confirmed stop and advances its generation even
// if the session was stopped while its task was already InReview.
func (d *DB) RecordSessionStop(id string) error {
	var previous, kind string
	err := d.WithTx(func(tx *sql.Tx) error {
		err := tx.QueryRow(`UPDATE tasks SET stop_generation=stop_generation+1 WHERE id=? RETURNING status, kind`, id).Scan(&previous, &kind)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrTaskNotFound, id)
		}
		if err != nil {
			return err
		}
		if kind == model.KindResident {
			return fmt.Errorf("%w (task %s)", ErrResidentProtected, id)
		}
		_, err = tx.Exec(`UPDATE tasks SET status=? WHERE id=?`, model.StatusInReview.String(), id)
		return err
	})
	if err == nil && previous != model.StatusInReview.String() {
		events.Emit(model.EventTypeTaskStatusChanged, id, map[string]string{
			"from": previous, "to": model.StatusInReview.String(),
		})
		events.EmitTaskUpdated(id, model.TaskFieldStatus)
	}
	return err
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
