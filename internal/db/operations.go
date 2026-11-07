package db

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrOperationNotFound = errors.New("operation: not found")
	// ErrOperationRequestReuse means request_id already names an operation with a different fingerprint.
	ErrOperationRequestReuse = errors.New("operation: request_id reused with a different request")
	// ErrOperationActive means the subject already has an active operation.
	ErrOperationActive = errors.New("operation: subject already has an active operation")
)

// Operation is a durable receipt for one accepted long-running request.
type Operation struct {
	ID          string
	RequestID   string
	Fingerprint string
	Kind        string
	SubjectType string
	SubjectID   string
	TaskID      string
	State       string
	Error       string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const operationCols = `id, request_id, fingerprint, kind, subject_type, subject_id, task_id, state, error, active, created_at, updated_at`

func scanOperation(row interface{ Scan(...any) error }) (*Operation, error) {
	var op Operation
	var active int
	var created, updated string
	err := row.Scan(&op.ID, &op.RequestID, &op.Fingerprint, &op.Kind, &op.SubjectType, &op.SubjectID,
		&op.TaskID, &op.State, &op.Error, &active, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	op.Active = active == 1
	op.CreatedAt = parseTime(created)
	op.UpdatedAt = parseTime(updated)
	return &op, nil
}

// CreateOperation records op, or returns the existing receipt when op.RequestID
// was already accepted with the same fingerprint (created=false).
func (d *DB) CreateOperation(op *Operation) (*Operation, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	existing, err := scanOperation(d.conn.QueryRow(`SELECT `+operationCols+` FROM operations WHERE request_id=?`, op.RequestID))
	switch {
	case err == nil:
		if existing.Fingerprint != op.Fingerprint {
			return nil, false, ErrOperationRequestReuse
		}
		return existing, false, nil
	case !errors.Is(err, ErrOperationNotFound):
		return nil, false, err
	}

	var n int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM operations WHERE subject_type=? AND subject_id=? AND active=1`,
		op.SubjectType, op.SubjectID).Scan(&n); err != nil {
		return nil, false, err
	}
	if n > 0 {
		return nil, false, ErrOperationActive
	}

	now := time.Now()
	op.Active = true
	op.CreatedAt, op.UpdatedAt = now, now
	_, err = d.conn.Exec(`INSERT INTO operations (`+operationCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		op.ID, op.RequestID, op.Fingerprint, op.Kind, op.SubjectType, op.SubjectID, op.TaskID, op.State, op.Error,
		formatTime(now), formatTime(now))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, false, ErrOperationActive
		}
		return nil, false, err
	}
	return op, true, nil
}

// Operation returns the receipt with the given id.
func (d *DB) Operation(id string) (*Operation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return scanOperation(d.conn.QueryRow(`SELECT `+operationCols+` FROM operations WHERE id=?`, id))
}

// ActiveOperation returns the active receipt of the given kind for a subject,
// or ErrOperationNotFound.
func (d *DB) ActiveOperation(subjectType, subjectID, kind string) (*Operation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return scanOperation(d.conn.QueryRow(`SELECT `+operationCols+` FROM operations
		WHERE subject_type=? AND subject_id=? AND kind=? AND active=1`, subjectType, subjectID, kind))
}

// TransitionOperation moves an active operation from state `from` to `to`.
// changed=false means it was no longer active in `from` (a lost race), which
// callers treat as already handled.
func (d *DB) TransitionOperation(id, from, to, errMsg string, active bool) (*Operation, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	a := 0
	if active {
		a = 1
	}
	res, err := d.conn.Exec(`UPDATE operations SET state=?, error=?, active=?, updated_at=?
		WHERE id=? AND state=? AND active=1`, to, errMsg, a, formatTime(time.Now()), id, from)
	if err != nil {
		return nil, false, err
	}
	n, _ := res.RowsAffected()
	op, err := scanOperation(d.conn.QueryRow(`SELECT `+operationCols+` FROM operations WHERE id=?`, id))
	if err != nil {
		return nil, false, err
	}
	return op, n > 0, nil
}

// ListActiveOperations returns every active receipt in the given state.
func (d *DB) ListActiveOperations(kind, state string) ([]*Operation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT `+operationCols+` FROM operations WHERE kind=? AND state=? AND active=1 ORDER BY created_at`, kind, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Operation
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

// OperationByRequestID returns the receipt accepted for requestID.
func (d *DB) OperationByRequestID(requestID string) (*Operation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return scanOperation(d.conn.QueryRow(`SELECT `+operationCols+` FROM operations WHERE request_id=?`, requestID))
}
