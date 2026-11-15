package db

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentTimerRejectsImplicitLocal(t *testing.T) {
	for _, zone := range []string{"Local", "local", "LOCAL"} {
		t.Run(zone, func(t *testing.T) {
			_, err := ResidentTimerNext("@every 15m", zone, time.Now())
			testutil.NotNil(t, err)
		})
	}
}

func TestResidentTimerBadRecurrenceDoesNotWedge(t *testing.T) {
	for _, kind := range []string{"zone missing", "no next leap day"} {
		t.Run(kind, func(t *testing.T) {
			d := testDB(t)
			now := time.Date(2096, 2, 28, 0, 0, 0, 0, time.UTC)
			expression := "@every 15m"
			due := now.Add(15 * time.Minute)
			if kind == "no next leap day" {
				expression = "0 0 29 2 *"
				due = now.Add(24 * time.Hour)
			}
			bad, err := d.SetResidentTimer("bad", time.Time{}, expression, "UTC", now)
			testutil.NoError(t, err)
			if kind == "zone missing" {
				_, err = d.conn.Exec(`UPDATE resident_timers SET timezone='missing/zone' WHERE id=?`, bad.ID)
				testutil.NoError(t, err)
			}
			_, err = d.SetResidentTimer("healthy", due.Add(time.Minute), "", "", now)
			testutil.NoError(t, err)
			fired, err := d.FireResidentTimers("solo", due.Add(2*time.Minute))
			testutil.NoError(t, err)
			testutil.Equal(t, len(fired), 1)
			testutil.Contains(t, fired[0].Body, "healthy")
			var active int
			testutil.NoError(t, d.conn.QueryRow(`SELECT active FROM resident_timers WHERE id=?`, bad.ID).Scan(&active))
			testutil.Equal(t, active, 0)
		})
	}
}

func TestResidentTimerOrphanReceiptRecovers(t *testing.T) {
	d := testDB(t)
	now := time.Now()
	_, err := d.SetResidentTimer("loop", time.Time{}, "@every 15m", "UTC", now)
	testutil.NoError(t, err)
	first, err := d.FireResidentTimers("solo", now.Add(16*time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, len(first), 1)
	_, err = d.conn.Exec(`DELETE FROM task_messages WHERE id=?`, first[0].ID)
	testutil.NoError(t, err)
	second, err := d.FireResidentTimers("solo", now.Add(32*time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, len(second), 1)
	testutil.False(t, first[0].ID == second[0].ID)
}

func TestResidentTimerSubmittedUnreadCoalesces(t *testing.T) {
	d := testDB(t)
	now := time.Now()
	_, err := d.SetResidentTimer("loop", time.Time{}, "@every 15m", "UTC", now)
	testutil.NoError(t, err)
	first, err := d.FireResidentTimers("solo", now.Add(16*time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, len(first), 1)
	testutil.NoError(t, d.MarkResidentTimerDelivered(first[0].ID))
	second, err := d.FireResidentTimers("solo", now.Add(32*time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, len(second), 0)
	pending, err := d.PendingResidentTimerDeliveries()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 1)
	testutil.True(t, pending[0].Submitted)
	_, err = d.AckMessages("solo", []string{first[0].ID})
	testutil.NoError(t, err)
	third, err := d.FireResidentTimers("solo", now.Add(48*time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, len(third), 1)
	msgs, err := d.Inbox("solo", InboxFilter{UnreadOnly: true})
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
}

func TestResidentTimerEmitsCommittedMessageEvent(t *testing.T) {
	d := testDB(t)
	now := time.Now()
	var captured []model.Event
	previous := events.SetSink(sinkFunc(func(ev model.Event) {
		msgs, err := d.Inbox("solo", InboxFilter{})
		testutil.NoError(t, err)
		testutil.Equal(t, len(msgs), 1)
		captured = append(captured, ev)
	}))
	t.Cleanup(func() { events.SetSink(previous) })
	_, err := d.SetResidentTimer("event", now.Add(time.Minute), "", "", now)
	testutil.NoError(t, err)
	msgs, err := d.FireResidentTimers("solo", now.Add(2*time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, len(captured), 1)
	testutil.Equal(t, captured[0].Type, model.EventTypeMessageSent)
	var payload map[string]string
	testutil.NoError(t, json.Unmarshal(captured[0].Payload, &payload))
	testutil.Equal(t, payload["id"], msgs[0].ID)
	testutil.Equal(t, payload["from"], model.SystemTaskID)
}

func TestResidentTimerPrunesOnlySettledTerminalHistory(t *testing.T) {
	d := testDB(t)
	now := time.Now()
	old := now.Add(-40 * 24 * time.Hour)
	fired, err := d.SetResidentTimer("settled", old.Add(time.Minute), "", "", old)
	testutil.NoError(t, err)
	unsettled, err := d.SetResidentTimer("unsettled", old.Add(time.Minute), "", "", old)
	testutil.NoError(t, err)
	msgs, err := d.FireResidentTimers("solo", old.Add(2*time.Minute))
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 2)
	_, err = d.AckMessages("solo", []string{msgs[0].ID})
	testutil.NoError(t, err)
	testutil.NoError(t, d.CompleteResidentTimerDelivery(msgs[0].ID))
	cancelled, err := d.SetResidentTimer("cancelled", now.Add(time.Hour), "", "", old)
	testutil.NoError(t, err)
	testutil.NoError(t, d.CancelResidentTimer(cancelled.ID))
	_, err = d.conn.Exec(`UPDATE resident_timers SET finished_at=? WHERE id=?`, old.UnixNano(), cancelled.ID)
	testutil.NoError(t, err)
	active, err := d.SetResidentTimer("active", now.Add(time.Hour), "", "", old)
	testutil.NoError(t, err)
	recent, err := d.SetResidentTimer("recent", now.Add(time.Hour), "", "", now)
	testutil.NoError(t, err)
	testutil.NoError(t, d.CancelResidentTimer(recent.ID))
	count, err := d.PruneResidentTimers(now)
	testutil.NoError(t, err)
	testutil.Equal(t, count, 2)
	for _, id := range []int64{fired.ID, cancelled.ID} {
		t.Run("removed", func(t *testing.T) {
			var n int
			testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM resident_timers WHERE id=?`, id).Scan(&n))
			testutil.Equal(t, n, 0)
		})
	}
	for _, id := range []int64{unsettled.ID, active.ID, recent.ID} {
		t.Run("kept", func(t *testing.T) {
			var n int
			testutil.NoError(t, d.conn.QueryRow(`SELECT COUNT(*) FROM resident_timers WHERE id=?`, id).Scan(&n))
			testutil.Equal(t, n, 1)
		})
	}
}
