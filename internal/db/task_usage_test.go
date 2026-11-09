package db

import (
	"fmt"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func i64(v int64) *int64 { return &v }

func TestUpsertTaskUsage_RetentionBounds(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { d.Close() }) //nolint:errcheck // test teardown
	base := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	var turns []UsageTurn
	for i := 0; i < MaxUsageTurnsPerSession+20; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		turns = append(turns, UsageTurn{TurnID: fmt.Sprintf("t%04d", i), StartedAt: at, MeasuredAt: at, UsageCounters: UsageCounters{InputTokens: i64(1)}})
	}
	_, err = d.UpsertTaskUsage(UsageSession{TaskID: "a", SessionID: "s", MeasuredAt: base}, turns)
	testutil.NoError(t, err)
	got, err := d.TaskUsageTurns("a", "s", 10000, false, 0, "")
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), MaxUsageTurnsPerSession)
	testutil.Equal(t, got[0].TurnID, fmt.Sprintf("t%04d", MaxUsageTurnsPerSession+19))

	for i := 0; i < MaxUsageSessionsPerTask+3; i++ {
		sid := fmt.Sprintf("sess%d", i)
		at := base.Add(time.Duration(i+1) * time.Hour)
		_, err = d.UpsertTaskUsage(UsageSession{TaskID: "b", SessionID: sid, MeasuredAt: at},
			[]UsageTurn{{TurnID: "x", StartedAt: at, MeasuredAt: at}})
		testutil.NoError(t, err)
	}
	var n int
	testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM task_usage_sessions WHERE task_id='b'`).Scan(&n))
	testutil.Equal(t, n, MaxUsageSessionsPerTask)
	testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM task_usage_turns WHERE task_id='b'`).Scan(&n))
	testutil.Equal(t, n, MaxUsageSessionsPerTask)
	old, err := d.TaskUsageSession("b", "sess0")
	testutil.NoError(t, err)
	testutil.True(t, old == nil)
}

func TestUpsertTaskUsage_ChangedFlag(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { d.Close() }) //nolint:errcheck // test teardown
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	s := UsageSession{TaskID: "a", SessionID: "s", MeasuredAt: at, UsageCounters: UsageCounters{InputTokens: i64(5)}}
	ch, err := d.UpsertTaskUsage(s, nil)
	testutil.NoError(t, err)
	testutil.True(t, ch)
	ch, _ = d.UpsertTaskUsage(s, nil)
	testutil.False(t, ch)
	s.MeasuredAt = at.Add(time.Second)
	ch, _ = d.UpsertTaskUsage(s, nil)
	testutil.True(t, ch)
}

func TestUpsertTaskUsage_UnchangedWritesNothing(t *testing.T) {
	for _, count := range []int{2, 500, 1200, 5000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			d := testDB(t)
			at := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
			s := UsageSession{TaskID: "a", SessionID: "s", MeasuredAt: at, UsageCounters: UsageCounters{InputTokens: i64(5)}}
			var turns []UsageTurn
			for i := 0; i < count; i++ {
				turns = append(turns, UsageTurn{TurnID: fmt.Sprint(i), StartedAt: at.Add(time.Duration(i) * time.Second), MeasuredAt: at})
			}
			_, err := d.UpsertTaskUsage(s, turns)
			testutil.NoError(t, err)
			var before, after int64
			testutil.NoError(t, d.conn.QueryRow("SELECT total_changes()").Scan(&before))
			ch, err := d.UpsertTaskUsage(s, turns)
			testutil.NoError(t, err)
			testutil.False(t, ch)
			testutil.NoError(t, d.conn.QueryRow("SELECT total_changes()").Scan(&after))
			testutil.Equal(t, after-before, int64(0))
		})
	}
}

func TestUpsertTaskUsage_TurnFreshness(t *testing.T) {
	d := testDB(t)
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	s := UsageSession{TaskID: "a", SessionID: "s", MeasuredAt: at}
	turn := UsageTurn{TurnID: "t", StartedAt: at, MeasuredAt: at, UsageCounters: UsageCounters{InputTokens: i64(10)}}
	_, err := d.UpsertTaskUsage(s, []UsageTurn{turn})
	testutil.NoError(t, err)
	s.MeasuredAt = at.Add(time.Minute)
	turn.MeasuredAt = at.Add(-time.Second)
	turn.InputTokens = i64(2)
	_, err = d.UpsertTaskUsage(s, []UsageTurn{turn})
	testutil.NoError(t, err)
	got, err := d.TaskUsageTurns("a", "s", 1, false, 0, "")
	testutil.NoError(t, err)
	testutil.Equal(t, len(got), 1)
	testutil.Equal(t, *got[0].InputTokens, int64(10))
}

func TestUpsertTaskUsage_UnmeasuredTurnWritesOnce(t *testing.T) {
	d := testDB(t)
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	s := UsageSession{TaskID: "a", SessionID: "s", MeasuredAt: at}
	turn := UsageTurn{TurnID: "t", StartedAt: at}
	changed, err := d.UpsertTaskUsage(s, []UsageTurn{turn})
	testutil.NoError(t, err)
	testutil.True(t, changed)
	var before, after int64
	testutil.NoError(t, d.conn.QueryRow("SELECT total_changes()").Scan(&before))
	changed, err = d.UpsertTaskUsage(s, []UsageTurn{turn})
	testutil.NoError(t, err)
	testutil.False(t, changed)
	testutil.NoError(t, d.conn.QueryRow("SELECT total_changes()").Scan(&after))
	testutil.Equal(t, after, before)
}
