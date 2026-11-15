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
