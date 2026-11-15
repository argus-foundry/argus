package db

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentTimerLazySchema(t *testing.T) {
	d := testDB(t)
	var count int
	testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='resident_timers'`).Scan(&count))
	testutil.Equal(t, count, 0)
	timers, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(timers), 0)
	testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='resident_timers'`).Scan(&count))
	testutil.Equal(t, count, 1)
}

func TestResidentTimerConcurrentInitialization(t *testing.T) {
	d := testDB(t)
	errors := make(chan error, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() { defer group.Done(); _, err := d.ResidentTimers(); errors <- err }()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		testutil.NoError(t, err)
	}
}

func TestResidentTimerSchemaRetriesAfterFailure(t *testing.T) {
	d := testDB(t)
	_, err := d.conn.Exec(`CREATE TABLE resident_timers(wrong_column TEXT)`)
	testutil.NoError(t, err)
	_, err = d.ResidentTimers()
	testutil.NotNil(t, err)
	testutil.False(t, d.residentTimersReady)
	_, err = d.conn.Exec(`DROP TABLE IF EXISTS resident_timer_deliveries; DROP TABLE resident_timers`)
	testutil.NoError(t, err)
	list, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 0)
	testutil.True(t, d.residentTimersReady)
}

func TestResidentTimerClosedStore(t *testing.T) {
	d := testDB(t)
	testutil.NoError(t, d.Close())
	for name, call := range map[string]func() error{
		"set": func() error {
			_, err := d.SetResidentTimer("check", time.Now().Add(time.Hour), "", "", time.Now())
			return err
		},
		"list":      func() error { _, err := d.ResidentTimers(); return err },
		"fire":      func() error { _, err := d.FireResidentTimers("solo", time.Now()); return err },
		"pending":   func() error { _, err := d.PendingResidentTimerDeliveries(); return err },
		"cancel":    func() error { return d.CancelResidentTimer(1) },
		"complete":  func() error { return d.CompleteResidentTimerDelivery("one") },
		"submitted": func() error { return d.MarkResidentTimerDelivered("one") },
		"prune":     func() error { _, err := d.PruneResidentTimers(time.Now()); return err },
	} {
		t.Run(name, func(t *testing.T) { testutil.NotNil(t, call()) })
	}
}

func TestResidentTimerDurableAndConcurrentFire(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "timers.sql")
	d, err := Open(path)
	testutil.NoError(t, err)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	_, err = d.SetResidentTimer("durable", now.Add(time.Hour), "", "", now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.Close())
	d, err = Open(path)
	testutil.NoError(t, err)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := d.FireResidentTimers("solo", now.Add(2*time.Hour))
			testutil.NoError(t, e)
		}()
	}
	wg.Wait()
	msgs, err := d.Inbox("solo", InboxFilter{})
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	testutil.NoError(t, d.Close())
	d, err = Open(path)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	pending, err := d.PendingResidentTimerDeliveries()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 1)
	testutil.Equal(t, pending[0].Message.ID, msgs[0].ID)
	testutil.NoError(t, d.MarkResidentTimerDelivered(msgs[0].ID))
	testutil.NoError(t, d.Close())
	d, err = Open(path)
	testutil.NoError(t, err)
	pending, err = d.PendingResidentTimerDeliveries()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 1)
	testutil.True(t, pending[0].Submitted)
}

func TestResidentTimerFireRollback(t *testing.T) {
	d := testDB(t)
	now := time.Now()
	_, err := d.SetResidentTimer("retry", now.Add(time.Minute), "", "", now)
	testutil.NoError(t, err)
	_, err = d.conn.Exec(`CREATE TRIGGER fail_timer_message BEFORE INSERT ON task_messages BEGIN SELECT RAISE(ABORT,'fail'); END`)
	testutil.NoError(t, err)
	_, err = d.FireResidentTimers("solo", now.Add(time.Hour))
	testutil.NotNil(t, err)
	timers, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(timers), 1)
	_, err = d.conn.Exec(`DROP TRIGGER fail_timer_message`)
	testutil.NoError(t, err)
	msgs, err := d.FireResidentTimers("solo", now.Add(time.Hour))
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
}

func TestResidentTimerSchedule(t *testing.T) {
	now := time.Date(2026, 3, 7, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ expression, zone, want string }{
		{"@every 15m", "America/Los_Angeles", "2026-03-07T09:15:00Z"},
		{"30 2 * * *", "America/Los_Angeles", "2026-03-07T10:30:00Z"},
		{"*/5 * * * *", "UTC", ""},
		{"0,5 8 * * *", "UTC", ""},
		{"@every 14m", "UTC", ""},
		{"@every 15m", "bad/timezone", ""},
		{"CRON_TZ=UTC 0 8 * * *", "UTC", ""},
		{"0 8 * * *", "", ""},
		{"bad cron", "UTC", ""},
		{"0 0 31 2 *", "UTC", ""},
		{"0,59 * * * *", "UTC", ""},
		{"@daily", "UTC", "2026-03-08T00:00:00Z"},
	} {
		t.Run(tc.expression+tc.zone, func(t *testing.T) {
			next, err := ResidentTimerNext(tc.expression, tc.zone, now)
			if tc.want == "" {
				testutil.NotNil(t, err)
				return
			}
			testutil.NoError(t, err)
			testutil.Equal(t, next.UTC().Format(time.RFC3339), tc.want)
		})
	}
	t.Run("spring gap skipped", func(t *testing.T) {
		next, err := ResidentTimerNext("30 2 * * *", "America/Los_Angeles", time.Date(2026, 3, 7, 11, 0, 0, 0, time.UTC))
		testutil.NoError(t, err)
		testutil.Equal(t, next.UTC().Format(time.RFC3339), "2026-03-09T09:30:00Z")
	})
	t.Run("fall repeated hour", func(t *testing.T) {
		next, err := ResidentTimerNext("30 1 * * *", "America/Los_Angeles", time.Date(2026, 11, 1, 8, 31, 0, 0, time.UTC))
		testutil.NoError(t, err)
		testutil.Equal(t, next.UTC().Format(time.RFC3339), "2026-11-01T09:30:00Z")
	})
}

func TestResidentTimerStore(t *testing.T) {
	d := testDB(t)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	timer, err := d.SetResidentTimer("check Sam", now.Add(time.Hour), "", "", now)
	testutil.NoError(t, err)
	recurring, err := d.SetResidentTimer("daily summary", now.Add(time.Hour), "@every 1h", "UTC", now)
	testutil.NoError(t, err)
	t.Run("no early fire", func(t *testing.T) {
		msgs, e := d.FireResidentTimers("solo", now)
		testutil.NoError(t, e)
		testutil.Equal(t, len(msgs), 0)
	})
	msgs, err := d.FireResidentTimers("solo", now.Add(8*time.Hour))
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 2)
	testutil.Contains(t, msgs[0].Body, "[argus] Timer")
	testutil.Contains(t, msgs[0].Body, "check Sam")
	testutil.Contains(t, msgs[0].Body, "was due at")
	testutil.Contains(t, msgs[0].Body, now.Format(time.RFC3339))
	msgs, err = d.FireResidentTimers("solo", now.Add(10*time.Hour))
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 0) // pending recurrence coalesces
	list, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 1)
	testutil.Equal(t, list[0].ID, recurring.ID)
	testutil.Equal(t, list[0].DueAt.After(now.Add(8*time.Hour)), true)
	testutil.Equal(t, list[0].DueAt.After(now.Add(10*time.Hour)), true)
	pending, err := d.PendingResidentTimerDeliveries()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 2)
	testutil.NoError(t, d.CompleteResidentTimerDelivery(pending[0].Message.ID))
	testutil.NoError(t, d.CancelResidentTimer(recurring.ID))
	pending, err = d.PendingResidentTimerDeliveries()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 1)
	testutil.Equal(t, pending[0].Cancelled, true)
	testutil.NoError(t, d.CancelResidentTimer(timer.ID)) // completed one-shot also cancellable
	testutil.NotNil(t, d.CancelResidentTimer(999999))
}

func TestResidentTimerLimits(t *testing.T) {
	d := testDB(t)
	now := time.Now()
	for _, note := range []string{"", "   ", strings.Repeat("a", 4097)} {
		t.Run("note"+note[:min(len(note), 3)], func(t *testing.T) {
			_, err := d.SetResidentTimer(note, now.Add(time.Hour), "", "", now)
			testutil.NotNil(t, err)
		})
	}
	_, err := d.SetResidentTimer("past", now, "", "", now)
	testutil.NotNil(t, err)
	_, err = d.SetResidentTimer("out of range", time.Date(2500, 1, 1, 0, 0, 0, 0, time.UTC), "", "", now)
	testutil.NotNil(t, err)
	for range 64 {
		_, err = d.SetResidentTimer("check", now.Add(time.Hour), "", "", now)
		testutil.NoError(t, err)
	}
	_, err = d.SetResidentTimer("overflow", now.Add(time.Hour), "", "", now)
	testutil.NotNil(t, err)
}
