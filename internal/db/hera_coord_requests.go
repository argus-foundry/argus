package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	heraCoordRequestPending = "pending"
	heraCoordRequestDone    = "done"
)

// HeraCoordRequestOutcome classifies a BeginHeraCoordinatorRequest call.
type HeraCoordRequestOutcome int

const (
	// HeraCoordRequestNew means the caller owns the receipt and must spawn.
	HeraCoordRequestNew HeraCoordRequestOutcome = iota
	// HeraCoordRequestReplay means an identical request already completed;
	// the stored response is returned and nothing may be spawned.
	HeraCoordRequestReplay
	// HeraCoordRequestInFlight means an identical request is still pending.
	HeraCoordRequestInFlight
	// HeraCoordRequestConflict means request_id was used with a different body.
	HeraCoordRequestConflict
)

// BeginHeraCoordinatorRequest atomically claims request_id for bodyHash. On a
// completed identical retry it returns the stored response JSON.
func (d *DB) BeginHeraCoordinatorRequest(requestID, bodyHash string) (HeraCoordRequestOutcome, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var hash, state, response string
	err := d.conn.QueryRow(
		`SELECT body_hash, state, response FROM hera_coordinator_requests WHERE request_id=?`, requestID,
	).Scan(&hash, &state, &response)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		now := formatTime(time.Now())
		if _, err := d.conn.Exec(
			`INSERT INTO hera_coordinator_requests (request_id, body_hash, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			requestID, bodyHash, heraCoordRequestPending, now, now,
		); err != nil {
			return 0, "", fmt.Errorf("claim coordinator request: %w", err)
		}
		return HeraCoordRequestNew, "", nil
	case err != nil:
		return 0, "", fmt.Errorf("read coordinator request: %w", err)
	case hash != bodyHash:
		return HeraCoordRequestConflict, "", nil
	case state == heraCoordRequestDone:
		return HeraCoordRequestReplay, response, nil
	}
	return HeraCoordRequestInFlight, "", nil
}

// CompleteHeraCoordinatorRequest stores the response and marks the receipt done.
func (d *DB) CompleteHeraCoordinatorRequest(requestID, responseJSON string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.conn.Exec(
		`UPDATE hera_coordinator_requests SET state=?, response=?, updated_at=? WHERE request_id=?`,
		heraCoordRequestDone, responseJSON, formatTime(time.Now()), requestID,
	)
	if err != nil {
		return fmt.Errorf("complete coordinator request: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("complete coordinator request: %q not found", requestID)
	}
	return nil
}

// ReleaseHeraCoordinatorRequest drops a pending receipt after a failed spawn so
// the same request_id can be retried. A completed receipt is never released.
func (d *DB) ReleaseHeraCoordinatorRequest(requestID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.conn.Exec(
		`DELETE FROM hera_coordinator_requests WHERE request_id=? AND state=?`, requestID, heraCoordRequestPending,
	); err != nil {
		return fmt.Errorf("release coordinator request: %w", err)
	}
	return nil
}
