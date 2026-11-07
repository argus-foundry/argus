package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Retention bounds keep a long-lived task's usage history from growing without
// limit: newest turns per session, newest sessions per task.
const (
	MaxUsageTurnsPerSession = 500
	MaxUsageSessionsPerTask = 8
)

// UsageCounters are token counts; nil is unreported, never zero. InputTokens
// includes CachedInputTokens.
type UsageCounters struct {
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
}

// UsageTurn is the stored spend of one turn.
type UsageTurn struct {
	TurnID     string
	StartedAt  time.Time
	MeasuredAt time.Time
	UsageCounters
}

// UsageSession is the stored totals of one session of one task.
type UsageSession struct {
	TaskID     string
	SessionID  string
	Account    string
	Provider   string
	Source     string
	MeasuredAt time.Time
	UsageCounters
}

func (d *DB) createTaskUsageTables() error {
	// Keyed by (task, session) so a replaced session never merges into its
	// successor. No FK to tasks: Delete cleans these up in its transaction,
	// matching task_meta. Timestamps are unix nanoseconds so ordering is exact.
	const ddl = `
		CREATE TABLE IF NOT EXISTS task_usage_sessions (
			task_id         TEXT NOT NULL,
			session_id      TEXT NOT NULL,
			account         TEXT NOT NULL DEFAULT '',
			provider        TEXT NOT NULL DEFAULT '',
			source          TEXT NOT NULL DEFAULT '',
			measured_at_ns  INTEGER NOT NULL,
			input_tokens    INTEGER,
			cached_tokens   INTEGER,
			output_tokens   INTEGER,
			PRIMARY KEY (task_id, session_id)
		);
		CREATE TABLE IF NOT EXISTS task_usage_turns (
			task_id         TEXT NOT NULL,
			session_id      TEXT NOT NULL,
			turn_id         TEXT NOT NULL,
			started_at_ns   INTEGER NOT NULL,
			measured_at_ns  INTEGER NOT NULL,
			input_tokens    INTEGER,
			cached_tokens   INTEGER,
			output_tokens   INTEGER,
			PRIMARY KEY (task_id, session_id, turn_id)
		);
		CREATE INDEX IF NOT EXISTS idx_task_usage_turns_order
			ON task_usage_turns(task_id, session_id, started_at_ns DESC, turn_id DESC);
	`
	if _, err := d.conn.Exec(ddl); err != nil {
		return fmt.Errorf("creating task usage tables: %w", err)
	}
	return nil
}

// UpsertTaskUsage records one observation of a session's totals and turns. It
// is idempotent: re-ingesting the same observation changes nothing and reports
// changed=false, and an observation older than the stored one is ignored so a
// stale read can never roll counters back. A turn row is replaced only by a
// measurement at least as new as its stored one.
func (d *DB) UpsertTaskUsage(s UsageSession, turns []UsageTurn) (bool, error) {
	changed := false
	err := d.WithTx(func(tx *sql.Tx) error {
		var storedNS int64
		err := tx.QueryRow(`SELECT measured_at_ns FROM task_usage_sessions WHERE task_id=? AND session_id=?`,
			s.TaskID, s.SessionID).Scan(&storedNS)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		case s.MeasuredAt.UnixNano() < storedNS:
			return nil
		}
		res, err := tx.Exec(`INSERT INTO task_usage_sessions
			(task_id, session_id, account, provider, source, measured_at_ns, input_tokens, cached_tokens, output_tokens)
			VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(task_id, session_id) DO UPDATE SET account=excluded.account, provider=excluded.provider,
			source=excluded.source, measured_at_ns=excluded.measured_at_ns, input_tokens=excluded.input_tokens,
			cached_tokens=excluded.cached_tokens, output_tokens=excluded.output_tokens
   WHERE task_usage_sessions.account IS NOT excluded.account OR task_usage_sessions.provider IS NOT excluded.provider
    OR task_usage_sessions.source IS NOT excluded.source OR task_usage_sessions.measured_at_ns IS NOT excluded.measured_at_ns
    OR task_usage_sessions.input_tokens IS NOT excluded.input_tokens OR task_usage_sessions.cached_tokens IS NOT excluded.cached_tokens
    OR task_usage_sessions.output_tokens IS NOT excluded.output_tokens`,
			s.TaskID, s.SessionID, s.Account, s.Provider, s.Source, s.MeasuredAt.UnixNano(),
			s.InputTokens, s.CachedInputTokens, s.OutputTokens)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		changed = n > 0
		turnChanged, err := upsertUsageTurns(tx, s, turns)
		if err != nil {
			return err
		}
		changed = changed || turnChanged
		if !changed {
			return nil
		}

		if _, err := tx.Exec(`DELETE FROM task_usage_turns WHERE task_id=? AND session_id=? AND turn_id NOT IN (
			SELECT turn_id FROM task_usage_turns WHERE task_id=? AND session_id=?
			ORDER BY started_at_ns DESC, turn_id DESC LIMIT ?)`,
			s.TaskID, s.SessionID, s.TaskID, s.SessionID, MaxUsageTurnsPerSession); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM task_usage_turns WHERE task_id=? AND session_id NOT IN (
			SELECT session_id FROM task_usage_sessions WHERE task_id=?
			ORDER BY measured_at_ns DESC, session_id DESC LIMIT ?)`,
			s.TaskID, s.TaskID, MaxUsageSessionsPerTask); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM task_usage_sessions WHERE task_id=? AND session_id NOT IN (
			SELECT session_id FROM task_usage_sessions WHERE task_id=?
			ORDER BY measured_at_ns DESC, session_id DESC LIMIT ?)`,
			s.TaskID, s.TaskID, MaxUsageSessionsPerTask)
		return err
	})
	return changed, err
}

// TaskUsageSession returns the stored totals of one session, or nil.
func (d *DB) TaskUsageSession(taskID, sessionID string) (*UsageSession, error) {
	return d.scanUsageSession(`SELECT task_id, session_id, account, provider, source, measured_at_ns, input_tokens, cached_tokens, output_tokens
		FROM task_usage_sessions WHERE task_id=? AND session_id=?`, taskID, sessionID)
}

// LatestTaskUsageSession returns the task's most recently measured session, or nil.
func (d *DB) LatestTaskUsageSession(taskID string) (*UsageSession, error) {
	return d.scanUsageSession(`SELECT task_id, session_id, account, provider, source, measured_at_ns, input_tokens, cached_tokens, output_tokens
		FROM task_usage_sessions WHERE task_id=? ORDER BY measured_at_ns DESC, session_id DESC LIMIT 1`, taskID)
}

func (d *DB) scanUsageSession(q string, args ...any) (*UsageSession, error) {
	var s UsageSession
	var ns int64
	var in, ca, out sql.NullInt64
	err := d.conn.QueryRow(q, args...).Scan(&s.TaskID, &s.SessionID, &s.Account, &s.Provider, &s.Source, &ns, &in, &ca, &out)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.MeasuredAt = time.Unix(0, ns).UTC()
	s.UsageCounters = nullCounters(in, ca, out)
	return &s, nil
}

func nullCounters(in, ca, out sql.NullInt64) UsageCounters {
	p := func(n sql.NullInt64) *int64 {
		if !n.Valid {
			return nil
		}
		v := n.Int64
		return &v
	}
	return UsageCounters{InputTokens: p(in), CachedInputTokens: p(ca), OutputTokens: p(out)}
}

// TaskUsageTurns returns up to limit turns newest first, strictly older than
// the (beforeNS, beforeID) position when hasBefore is set.
func (d *DB) TaskUsageTurns(taskID, sessionID string, limit int, hasBefore bool, beforeNS int64, beforeID string) ([]UsageTurn, error) {
	q := `SELECT turn_id, started_at_ns, measured_at_ns, input_tokens, cached_tokens, output_tokens
		FROM task_usage_turns WHERE task_id=? AND session_id=?`
	args := []any{taskID, sessionID}
	if hasBefore {
		q += ` AND (started_at_ns < ? OR (started_at_ns = ? AND turn_id < ?))`
		args = append(args, beforeNS, beforeNS, beforeID)
	}
	q += ` ORDER BY started_at_ns DESC, turn_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return scanUsageTurns(rows)
}

func scanUsageTurns(rows *sql.Rows) ([]UsageTurn, error) {
	defer rows.Close() //nolint:errcheck // read-only rows
	var out []UsageTurn
	for rows.Next() {
		var t UsageTurn
		var sNS, mNS int64
		var in, ca, o sql.NullInt64
		if err := rows.Scan(&t.TurnID, &sNS, &mNS, &in, &ca, &o); err != nil {
			return nil, err
		}
		t.StartedAt, t.MeasuredAt = time.Unix(0, sNS).UTC(), time.Unix(0, mNS).UTC()
		t.UsageCounters = nullCounters(in, ca, o)
		out = append(out, t)
	}
	return out, rows.Err()
}
