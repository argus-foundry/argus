package db

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentBuiltinTimerRestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "timers.sql")
	d, err := Open(file)
	testutil.NoError(t, err)
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	testutil.NoError(t, d.EnsureResidentMemoryTidy("tidy", "30 2 * * *", "UTC", now))
	before, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.NoError(t, d.Close())
	d, err = Open(file)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- d.EnsureResidentMemoryTidy("tidy", "30 2 * * *", "UTC", now.Add(24*time.Hour)) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		testutil.NoError(t, err)
	}
	after, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.DeepEqual(t, after, before)
}

func TestResidentBuiltinTimerErrors(t *testing.T) {
	d := testDB(t)
	testutil.NotNil(t, d.EnsureResidentMemoryTidy("tidy", "bad", "UTC", time.Now()))
	testutil.NoError(t, d.Close())
	testutil.NotNil(t, d.EnsureResidentMemoryTidy("tidy", "30 2 * * *", "UTC", time.Now()))
}

func TestResidentBuiltinTimer(t *testing.T) {
	d := testDB(t)
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	testutil.NoError(t, d.EnsureResidentMemoryTidy("tidy memory", "30 2 * * *", "UTC", now))
	list, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 1)
	id, due := list[0].ID, list[0].DueAt
	testutil.Equal(t, list[0].Builtin, "memory_tidy")
	testutil.Contains(t, d.CancelResidentTimer(id).Error(), "config")
	// Reconciliation after the due time must not move it forward before firing.
	testutil.NoError(t, d.EnsureResidentMemoryTidy("tidy memory", "30 2 * * *", "UTC", now.Add(24*time.Hour)))
	list, err = d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, list[0].ID, id)
	testutil.Equal(t, list[0].DueAt, due)
	msgs, err := d.FireResidentTimers("solo", now.Add(24*time.Hour))
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	testutil.Contains(t, msgs[0].Body, "tidy memory")
	// A schedule change retracts the old queued fire and keeps the identity.
	testutil.NoError(t, d.EnsureResidentMemoryTidy("new procedure", "0 3 * * *", "UTC", now.Add(24*time.Hour)))
	pending, err := d.PendingResidentTimerDeliveries()
	testutil.NoError(t, err)
	testutil.False(t, pending[0].Message.ReadAt.IsZero())
	list, err = d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, list[0].ID, id)
	testutil.Equal(t, list[0].Note, "new procedure")
	testutil.Equal(t, list[0].Expression, "0 3 * * *")
	testutil.Equal(t, list[0].Timezone, "UTC")
	testutil.Equal(t, list[0].DueAt, now.Add(27*time.Hour))
	testutil.NoError(t, d.EnsureResidentMemoryTidy("", "", "", now))
	var disabledDue int64
	testutil.NoError(t, d.conn.QueryRow(`SELECT due_at FROM resident_timers WHERE id=?`, id).Scan(&disabledDue))
	testutil.Equal(t, disabledDue, int64(0))
	list, err = d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 0)
	pending, err = d.PendingResidentTimerDeliveries()
	testutil.NoError(t, err)
	testutil.True(t, pending[0].Cancelled)
	testutil.NoError(t, d.EnsureResidentMemoryTidy("tidy memory", "30 2 * * *", "UTC", now))
	list, err = d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, list[0].ID, id)
}

func TestResidentBuiltinTimerNoteRefresh(t *testing.T) {
	d := testDB(t)
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	testutil.NoError(t, d.EnsureResidentMemoryTidy("old folder", "30 2 * * *", "UTC", now))
	before, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.NoError(t, d.EnsureResidentMemoryTidy("new folder", "30 2 * * *", "UTC", now.Add(time.Hour)))
	after, err := d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, after[0].DueAt, before[0].DueAt)
	msgs, err := d.FireResidentTimers("solo", before[0].DueAt)
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	testutil.Contains(t, msgs[0].Body, "new folder")
	before, err = d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.NoError(t, d.EnsureResidentMemoryTidy("latest folder", "30 2 * * *", "UTC", now.Add(48*time.Hour)))
	after, err = d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, after[0].DueAt, before[0].DueAt)
	testutil.Equal(t, after[0].Note, "latest folder")
	pending, err := d.PendingResidentTimerDeliveries()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 1)
	testutil.True(t, pending[0].Message.ReadAt.IsZero())
	testutil.Equal(t, pending[0].Message.Body, msgs[0].Body)
	testutil.NoError(t, d.CompleteResidentTimerDelivery(msgs[0].ID))
	msgs, err = d.FireResidentTimers("solo", now.Add(48*time.Hour))
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	testutil.Contains(t, msgs[0].Body, "latest folder")
}

func TestResidentBuiltinTimerRejectsLocal(t *testing.T) {
	d := testDB(t)
	testutil.NotNil(t, d.EnsureResidentMemoryTidy("tidy", "30 2 * * *", "Local", time.Now()))
}

func TestResidentBuiltinTimerSurvivesPruning(t *testing.T) {
	d := testDB(t)
	now := time.Now()
	testutil.NoError(t, d.EnsureResidentMemoryTidy("tidy", "30 2 * * *", "UTC", now))
	list, err := d.ResidentTimers()
	testutil.NoError(t, err)
	id := list[0].ID
	personal, err := d.SetResidentTimer("personal", now.Add(time.Hour), "", "", now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.CancelResidentTimer(personal.ID))
	// Model a built-in disabled by the invalid-recurrence recovery path.
	_, err = d.conn.Exec(`UPDATE resident_timers SET active=0,finished_at=?`, now.Add(-31*24*time.Hour).UnixNano())
	testutil.NoError(t, err)
	count, err := d.PruneResidentTimers(now)
	testutil.NoError(t, err)
	testutil.Equal(t, count, int64(1))
	testutil.NoError(t, d.EnsureResidentMemoryTidy("tidy", "30 2 * * *", "UTC", now))
	list, err = d.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 1)
	testutil.Equal(t, list[0].ID, id)
}

func TestResidentBuiltinTimerAllowance(t *testing.T) {
	d := testDB(t)
	now := time.Now()
	testutil.NoError(t, d.EnsureResidentMemoryTidy("tidy", "30 2 * * *", "UTC", now))
	for range ResidentTimerLimit {
		_, err := d.SetResidentTimer("follow up", now.Add(time.Hour), "", "", now)
		testutil.NoError(t, err)
	}
	_, err := d.SetResidentTimer("overflow", now.Add(time.Hour), "", "", now)
	testutil.NotNil(t, err)
}
