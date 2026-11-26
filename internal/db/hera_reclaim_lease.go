package db

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// HeraReclaimLease records the origin ref that resident cleanup may delete.
// An empty tip means only local resources may be reclaimed.
type HeraReclaimLease struct {
	RepoDir string `json:"repo_dir"`
	Branch  string `json:"branch"`
	Tip     string `json:"tip"`
}

func encodeHeraReclaimLease(lease *HeraReclaimLease) (string, error) {
	if lease == nil {
		return "", nil
	}
	raw, err := json.Marshal(lease)
	return string(raw), err
}

// HeraResidentReclaimLease reads the original worker-deletion lease. A nil
// result identifies a regular nuke rather than resident cleanup.
func (d *DB) HeraResidentReclaimLease(taskID string) (*HeraReclaimLease, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var raw string
	err := d.conn.QueryRow(`SELECT resident_reclaim_lease FROM hera_bindings
		WHERE argus_task_id=? AND nuke_reclaim='claimed' AND resident_reclaim_lease<>''
		AND EXISTS (SELECT 1 FROM tasks WHERE id=argus_task_id)
		ORDER BY id DESC LIMIT 1`, taskID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lease HeraReclaimLease
	if err := json.Unmarshal([]byte(raw), &lease); err != nil {
		return nil, err
	}
	return &lease, nil
}

// HasPendingResidentReclaims reports whether the fast recovery tick should
// try deferred resident cleanup after normal exit finalization.
func (d *DB) HasPendingResidentReclaims() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var pending bool
	err := d.conn.QueryRow(`SELECT EXISTS (SELECT 1 FROM hera_bindings b
		JOIN tasks t ON t.id=b.argus_task_id
		WHERE b.nuke_reclaim='claimed' AND b.resident_reclaim_lease<>'')`).Scan(&pending)
	return pending, err
}
