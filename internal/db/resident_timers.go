package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const ResidentTimerLimit = 64
const ResidentTimerNoteLimit = 4096
const ResidentTimerMinInterval = 15 * time.Minute

// ensureResidentTimerSchema avoids charging every ordinary task DB (and every
// test fixture) for resident-only tables. Existing rows survive normally: the
// first use after a reopen only checks the schema, never resets timer state.
func (d *DB) ensureResidentTimerSchema() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.residentTimersReady {
		return nil
	}
	_, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS resident_timers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			note TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			due_at INTEGER NOT NULL,
			expression TEXT NOT NULL DEFAULT '',
			timezone TEXT NOT NULL DEFAULT '',
			active INTEGER NOT NULL DEFAULT 1,
			cancelled INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS resident_timer_deliveries (
			timer_id INTEGER PRIMARY KEY REFERENCES resident_timers(id),
			message_id TEXT NOT NULL UNIQUE
		);
		CREATE INDEX IF NOT EXISTS idx_resident_timers_due ON resident_timers(due_at,id) WHERE active=1;
	`)
	if err == nil {
		d.residentTimersReady = true
	}
	return err
}

// ResidentTimer belongs to the singleton resident, independently of its session.
type ResidentTimer struct {
	ID         int64     `json:"id"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"created_at"`
	DueAt      time.Time `json:"due_at"`
	Expression string    `json:"expression,omitempty"`
	Timezone   string    `json:"timezone,omitempty"`
}

// ResidentTimerNext validates a bounded local schedule and returns its next fire.
func ResidentTimerNext(expression, zone string, after time.Time) (time.Time, error) {
	if zone == "" || strings.Contains(expression, "TZ=") {
		return time.Time{}, errors.New("provide an IANA timezone separately from the expression")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, err
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	sched, err := parser.Parse("CRON_TZ=" + loc.String() + " " + expression)
	if err != nil {
		return time.Time{}, err
	}
	switch s := sched.(type) {
	case cron.ConstantDelaySchedule:
		if s.Delay < ResidentTimerMinInterval {
			return time.Time{}, errors.New("recurrence must be at least 15 minutes")
		}
	case *cron.SpecSchedule:
		// Conservatively check every permitted clock minute, ignoring calendar
		// restrictions. This also covers sparse schedules years in the future.
		first, last := -1, -1
		for h := 0; h < 24; h++ {
			for m := 0; m < 60; m++ {
				if s.Hour&(uint64(1)<<h) == 0 || s.Minute&(uint64(1)<<m) == 0 {
					continue
				}
				v := h*60 + m
				if last >= 0 && v-last < 15 {
					return time.Time{}, errors.New("recurrence must be at least 15 minutes")
				}
				if first < 0 {
					first = v
				}
				last = v
			}
		}
		if first < 0 || 1440-last+first < 15 {
			return time.Time{}, errors.New("recurrence must be at least 15 minutes")
		}
	}
	next := sched.Next(after)
	if next.IsZero() {
		return time.Time{}, errors.New("schedule has no next occurrence")
	}
	return next, nil
}

// SetResidentTimer validates and inserts under the same lock as the active cap.
func (d *DB) SetResidentTimer(note string, due time.Time, expression, zone string, now time.Time) (*ResidentTimer, error) {
	if strings.TrimSpace(note) == "" || len(note) > ResidentTimerNoteLimit {
		return nil, errors.New("note must contain 1 to 4096 bytes of nonblank text")
	}
	if expression != "" {
		var err error
		due, err = ResidentTimerNext(expression, zone, now)
		if err != nil {
			return nil, err
		}
	} else if !due.After(now) {
		return nil, errors.New("timer must be in the future")
	}
	if !time.Unix(0, due.UnixNano()).Equal(due) {
		return nil, errors.New("timer time is outside the supported range")
	}
	if err := d.ensureResidentTimerSchema(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var count int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM resident_timers WHERE active=1`).Scan(&count); err != nil {
		return nil, err
	}
	if count >= ResidentTimerLimit {
		return nil, errors.New("active timer limit reached (64)")
	}
	res, err := d.conn.Exec(`INSERT INTO resident_timers(note,created_at,due_at,expression,timezone) VALUES(?,?,?,?,?)`, note, now.UnixNano(), due.UnixNano(), expression, zone)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	uxlog.Log("[resident-timer] set id=%d due=%s recurring=%t", id, due.Format(time.RFC3339), expression != "")
	return &ResidentTimer{ID: id, Note: note, CreatedAt: now.UTC(), DueAt: due.UTC(), Expression: expression, Timezone: zone}, nil
}

func (d *DB) ResidentTimers() ([]ResidentTimer, error) {
	if err := d.ensureResidentTimerSchema(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT id,note,created_at,due_at,expression,timezone FROM resident_timers WHERE active=1 ORDER BY due_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ResidentTimer{}
	for rows.Next() {
		var t ResidentTimer
		var created, due int64
		if err := rows.Scan(&t.ID, &t.Note, &created, &due, &t.Expression, &t.Timezone); err != nil {
			return nil, err
		}
		t.CreatedAt = time.Unix(0, created).UTC()
		t.DueAt = time.Unix(0, due).UTC()
		out = append(out, t)
	}
	return out, rows.Err()
}

// CancelResidentTimer also acknowledges a queued fire. The daemon cancels its
// notifier registration before removing the durable pending-delivery record.
func (d *DB) CancelResidentTimer(id int64) error {
	if err := d.ensureResidentTimerSchema(); err != nil {
		return err
	}
	err := d.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE resident_timers SET active=0,cancelled=1 WHERE id=?`, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return errors.New("timer not found")
		}
		_, err = tx.Exec(`UPDATE task_messages SET read_at=? WHERE id IN (SELECT message_id FROM resident_timer_deliveries WHERE timer_id=?) AND read_at=''`, formatTime(time.Now()), id)
		return err
	})
	if err == nil {
		uxlog.Log("[resident-timer] cancel id=%d", id)
	}
	return err
}

// FireResidentTimers is the exactly-once inbox boundary. Advancing the timer,
// persisting its event and retaining a delivery receipt are one transaction.
func (d *DB) FireResidentTimers(taskID string, now time.Time) ([]*model.TaskMessage, error) {
	timers, err := d.ResidentTimers()
	if err != nil {
		return nil, err
	}
	var out []*model.TaskMessage
	for _, t := range timers {
		if t.DueAt.After(now) {
			break
		}
		var msg *model.TaskMessage
		err = d.WithTx(func(tx *sql.Tx) error {
			var active, pending int
			var due int64
			if e := tx.QueryRow(`SELECT active,due_at,(SELECT COUNT(*) FROM resident_timer_deliveries WHERE timer_id=resident_timers.id) FROM resident_timers WHERE id=?`, t.ID).Scan(&active, &due, &pending); e != nil {
				return e
			}
			if active == 0 || due != t.DueAt.UnixNano() {
				uxlog.Log("[resident-timer] skip id=%d pending=%d", t.ID, pending)
				return nil
			}
			next := t.DueAt
			enabled := 0
			if t.Expression != "" {
				var e error
				next, e = ResidentTimerNext(t.Expression, t.Timezone, now)
				if e != nil {
					return e
				}
				enabled = 1
			}
			if pending > 0 {
				uxlog.Log("[resident-timer] skip id=%d coalesced pending fire", t.ID)
				_, e := tx.Exec(`UPDATE resident_timers SET due_at=? WHERE id=?`, next.UnixNano(), t.ID)
				return e
			}
			msg = &model.TaskMessage{ID: generateID(), From: model.SystemTaskID, To: taskID, Kind: model.KindNote, CreatedAt: now, Body: fmt.Sprintf("[argus] Timer #%d (set at %s; was due at %s):\n%s", t.ID, t.CreatedAt.Format(time.RFC3339), t.DueAt.Format(time.RFC3339), t.Note)}
			if _, e := tx.Exec(`INSERT INTO task_messages(id,from_task_id,to_task_id,kind,body,in_reply_to,created_at,read_at) VALUES(?,?,?,?,?,'',?,'')`, msg.ID, msg.From, msg.To, string(msg.Kind), msg.Body, formatTime(now)); e != nil {
				return e
			}
			if _, e := tx.Exec(`INSERT INTO resident_timer_deliveries(timer_id,message_id) VALUES(?,?)`, t.ID, msg.ID); e != nil {
				return e
			}
			_, e := tx.Exec(`UPDATE resident_timers SET active=?,due_at=? WHERE id=?`, enabled, next.UnixNano(), t.ID)
			return e
		})
		if err != nil {
			return out, err
		}
		if msg != nil {
			out = append(out, msg)
			uxlog.Log("[resident-timer] fire id=%d message=%s", t.ID, msg.ID)
		}
	}
	return out, nil
}

type ResidentTimerDelivery struct {
	Message   model.TaskMessage
	Cancelled bool
}

func (d *DB) PendingResidentTimerDeliveries() ([]ResidentTimerDelivery, error) {
	if err := d.ensureResidentTimerSchema(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.conn.Query(`SELECT m.id,m.to_task_id,m.body,m.read_at,t.cancelled FROM resident_timer_deliveries p JOIN resident_timers t ON t.id=p.timer_id JOIN task_messages m ON m.id=p.message_id ORDER BY t.due_at,t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResidentTimerDelivery
	for rows.Next() {
		var p ResidentTimerDelivery
		var read string
		if e := rows.Scan(&p.Message.ID, &p.Message.To, &p.Message.Body, &read, &p.Cancelled); e != nil {
			return nil, e
		}
		if read != "" {
			p.Message.ReadAt = parseTime(read)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) CompleteResidentTimerDelivery(messageID string) error {
	if err := d.ensureResidentTimerSchema(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.conn.Exec(`DELETE FROM resident_timer_deliveries WHERE message_id=?`, messageID)
	return err
}
