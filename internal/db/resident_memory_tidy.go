package db

import (
	"database/sql"
	"errors"
	"time"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// EnsureResidentMemoryTidy reconciles the single config-owned timer. Unchanged
// schedules retain due_at so restarts cannot postpone an overdue fire forever.
func (d *DB) EnsureResidentMemoryTidy(note, expression, zone string, now time.Time) error {
	var due time.Time
	if expression != "" {
		var err error
		due, err = ResidentTimerNext(expression, zone, now)
		if err != nil {
			return err
		}
	}
	if err := d.ensureResidentTimerSchema(); err != nil {
		return err
	}
	return d.WithTx(func(tx *sql.Tx) error {
		var id int64
		var oldNote, oldExpr, oldZone string
		var active int
		err := tx.QueryRow(`SELECT t.id,t.note,t.expression,t.timezone,t.active FROM resident_builtin_timers b JOIN resident_timers t ON t.id=b.timer_id WHERE b.name='memory_tidy'`).Scan(&id, &oldNote, &oldExpr, &oldZone, &active)
		if errors.Is(err, sql.ErrNoRows) {
			if expression == "" {
				return nil
			}
			result, e := tx.Exec(`INSERT INTO resident_timers(note,created_at,due_at,expression,timezone) VALUES(?,?,?,?,?)`, note, now.UnixNano(), due.UnixNano(), expression, zone)
			if e != nil {
				return e
			}
			id, e = result.LastInsertId()
			if e != nil {
				return e
			}
			_, e = tx.Exec(`INSERT INTO resident_builtin_timers(name,timer_id) VALUES('memory_tidy',?)`, id)
			if e == nil {
				uxlog.Log("[resident-memory-tidy] created timer=%d due=%s", id, due.Format(time.RFC3339))
			}
			return e
		}
		if err != nil {
			return err
		}
		enabled := 0
		if expression != "" {
			enabled = 1
		}
		if active == enabled && (enabled == 0 || (oldExpr == expression && oldZone == zone)) {
			if enabled == 1 && oldNote != note {
				// Queued messages keep their original instructions; future fires
				// use the new note without postponing an overdue occurrence.
				_, err = tx.Exec(`UPDATE resident_timers SET note=? WHERE id=?`, note, id)
				if err == nil {
					uxlog.Log("[resident-memory-tidy] refreshed note timer=%d pending work preserved", id)
				}
				return err
			}
			return nil
		}
		// The existing notifier receipt sees read/cancelled and retracts any
		// unsubmitted wake-up. Keep it until that cancellation has completed.
		if _, err = tx.Exec(`UPDATE task_messages SET read_at=? WHERE id IN (SELECT message_id FROM resident_timer_deliveries WHERE timer_id=?) AND read_at=''`, formatTime(now), id); err != nil {
			return err
		}
		var dueAt int64
		if enabled == 1 {
			dueAt = due.UnixNano()
		}
		_, err = tx.Exec(`UPDATE resident_timers SET note=?,expression=?,timezone=?,due_at=?,active=?,cancelled=? WHERE id=?`, note, expression, zone, dueAt, enabled, 1-enabled, id)
		if err == nil {
			uxlog.Log("[resident-memory-tidy] reconciled timer=%d enabled=%t due=%s", id, enabled == 1, due.Format(time.RFC3339))
		}
		return err
	})
}
