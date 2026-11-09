package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrDeleteJobNotFound is returned when a task-delete job id is unknown.
var ErrDeleteJobNotFound = errors.New("task delete job not found")

// TaskDeleteJob is one persisted bulk-delete operation. Results is an opaque
// JSON document owned by the API layer.
type TaskDeleteJob struct {
	ID             string
	IdempotencyKey string
	RequestHash    string
	Status         string
	Results        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const deleteJobColumns = `id, COALESCE(idempotency_key,''), request_hash, status, results, created_at, updated_at`

func scanDeleteJob(sc interface{ Scan(...any) error }) (*TaskDeleteJob, error) {
	var j TaskDeleteJob
	var created, updated string
	if err := sc.Scan(&j.ID, &j.IdempotencyKey, &j.RequestHash, &j.Status, &j.Results, &created, &updated); err != nil {
		return nil, err
	}
	j.CreatedAt, j.UpdatedAt = parseTime(created), parseTime(updated)
	return &j, nil
}

// CreateTaskDeleteJob inserts job unless its idempotency key already exists,
// in which case the stored job is returned with created=false. A blank key
// never deduplicates.
func (d *DB) CreateTaskDeleteJob(job TaskDeleteJob) (stored *TaskDeleteJob, created bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if job.IdempotencyKey != "" {
		row := d.conn.QueryRow(`SELECT `+deleteJobColumns+` FROM task_delete_jobs WHERE idempotency_key=?`, job.IdempotencyKey)
		ex, serr := scanDeleteJob(row)
		if serr == nil {
			return ex, false, nil
		}
		if !errors.Is(serr, sql.ErrNoRows) {
			return nil, false, serr
		}
	}
	now := time.Now()
	var key any
	if job.IdempotencyKey != "" {
		key = job.IdempotencyKey
	}
	if _, err := d.conn.Exec(
		`INSERT INTO task_delete_jobs (id, idempotency_key, request_hash, status, results, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		job.ID, key, job.RequestHash, job.Status, job.Results, formatTime(now), formatTime(now)); err != nil {
		return nil, false, fmt.Errorf("insert task delete job: %w", err)
	}
	job.CreatedAt, job.UpdatedAt = now, now
	return &job, true, nil
}

// GetTaskDeleteJob returns the job or ErrDeleteJobNotFound.
func (d *DB) GetTaskDeleteJob(id string) (*TaskDeleteJob, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	j, err := scanDeleteJob(d.conn.QueryRow(`SELECT `+deleteJobColumns+` FROM task_delete_jobs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrDeleteJobNotFound, id)
	}
	return j, err
}

// SaveTaskDeleteJob overwrites a job's status and results.
func (d *DB) SaveTaskDeleteJob(id, status, results string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.conn.Exec(`UPDATE task_delete_jobs SET status=?, results=?, updated_at=? WHERE id=?`,
		status, results, formatTime(time.Now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %s", ErrDeleteJobNotFound, id)
	}
	return nil
}

// ListUnfinishedTaskDeleteJobs returns every job whose status is not complete,
// oldest first, for restart recovery.
func (d *DB) ListUnfinishedTaskDeleteJobs() ([]*TaskDeleteJob, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT ` + deleteJobColumns + ` FROM task_delete_jobs WHERE status != 'complete' ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TaskDeleteJob
	for rows.Next() {
		j, err := scanDeleteJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// TaskHoldsLiveHeraCoordinatorBinding reports whether taskID has a live
// binding to a coordinator-kind role.
func (d *DB) TaskHoldsLiveHeraCoordinatorBinding(taskID string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var n int
	err := d.conn.QueryRow(
		`SELECT COUNT(*) FROM hera_bindings b JOIN hera_roles r ON r.id = b.role_id
		 WHERE b.argus_task_id=? AND b.ended_at IS NULL AND r.kind=?`,
		taskID, string(HeraKindCoordinator)).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("task holds live coordinator binding: %w", err)
	}
	return n > 0, nil
}
