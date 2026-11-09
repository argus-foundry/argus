package db

import (
	"cmp"
	"database/sql"
	"slices"
)

func upsertUsageTurns(tx *sql.Tx, s UsageSession, turns []UsageTurn) (bool, error) {
	rows, err := tx.Query(`SELECT turn_id, started_at_ns, measured_at_ns, input_tokens, cached_tokens, output_tokens
  FROM task_usage_turns WHERE task_id=? AND session_id=?`, s.TaskID, s.SessionID)
	if err != nil {
		return false, err
	}
	stored, err := scanUsageTurns(rows)
	if err != nil {
		return false, err
	}
	existing := make(map[string]UsageTurn, len(stored))
	retained := make(map[string]UsageTurn, len(stored)+len(turns))
	for _, t := range stored {
		existing[t.TurnID] = t
		retained[t.TurnID] = t
	}
	for _, t := range turns {
		old, ok := retained[t.TurnID]
		if !ok || !t.MeasuredAt.Before(old.MeasuredAt) {
			retained[t.TurnID] = t
		}
	}
	ordered := make([]UsageTurn, 0, len(retained))
	for _, t := range retained {
		ordered = append(ordered, t)
	}
	slices.SortFunc(ordered, func(a, b UsageTurn) int {
		if n := b.StartedAt.Compare(a.StartedAt); n != 0 {
			return n
		}
		return cmp.Compare(b.TurnID, a.TurnID)
	})
	keep := make(map[string]bool, min(len(ordered), MaxUsageTurnsPerSession))
	for _, t := range ordered[:min(len(ordered), MaxUsageTurnsPerSession)] {
		keep[t.TurnID] = true
	}
	changed := false
	for _, t := range turns {
		if !keep[t.TurnID] {
			continue
		}
		if old, ok := existing[t.TurnID]; ok && sameUsageTurn(old, t) {
			continue
		}
		res, err := tx.Exec(`INSERT INTO task_usage_turns
   (task_id, session_id, turn_id, started_at_ns, measured_at_ns, input_tokens, cached_tokens, output_tokens)
   VALUES (?,?,?,?,?,?,?,?)
   ON CONFLICT(task_id, session_id, turn_id) DO UPDATE SET started_at_ns=excluded.started_at_ns,
   measured_at_ns=excluded.measured_at_ns, input_tokens=excluded.input_tokens,
   cached_tokens=excluded.cached_tokens, output_tokens=excluded.output_tokens
   WHERE excluded.measured_at_ns >= task_usage_turns.measured_at_ns`,
			s.TaskID, s.SessionID, t.TurnID, t.StartedAt.UnixNano(), t.MeasuredAt.UnixNano(),
			t.InputTokens, t.CachedInputTokens, t.OutputTokens)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		changed = changed || n > 0
	}
	return changed, nil
}

func sameUsageTurn(a, b UsageTurn) bool {
	eq := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.StartedAt.UnixNano() == b.StartedAt.UnixNano() && a.MeasuredAt.UnixNano() == b.MeasuredAt.UnixNano() &&
		eq(a.InputTokens, b.InputTokens) && eq(a.CachedInputTokens, b.CachedInputTokens) && eq(a.OutputTokens, b.OutputTokens)
}
