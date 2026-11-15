package daemon

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/notify"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentTimerStateReadErrorLogged(t *testing.T) {
	e := newResEnv(t)
	var log bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	testutil.NoError(t, e.db.Close())
	e.m.State()
	testutil.Contains(t, log.String(), "[resident-timer] state read failed")
}

func TestResidentTimerOrphanAndExpiredRegistration(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	_, err := e.db.SetResidentTimer("loop", time.Time{}, "@every 15m", "UTC", e.now)
	testutil.NoError(t, err)
	e.now = e.now.Add(16 * time.Minute)
	n := &timerNotifier{calls: map[string]string{}, submitted: map[string]bool{}}
	e.m.notifier = func() residentNotifier { return n }
	e.m.deliverTimers()
	testutil.Equal(t, n.callCount, 1)
	// A notifier deadline removes its registration; the durable receipt retries.
	for id := range n.calls {
		delete(n.calls, id)
	}
	e.m.deliverTimers()
	testutil.Equal(t, n.callCount, 2)
	res, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	// Deleting the resident's inbox is a supported task lifecycle operation.
	_, err = e.db.DeleteMessagesForTask(res.ID)
	testutil.NoError(t, err)
	e.now = e.now.Add(16 * time.Minute)
	e.m.deliverTimers()
	testutil.Equal(t, len(n.cancelled), 1)
	testutil.Equal(t, n.callCount, 3)
}

func TestResidentTimerFireErrorStillDelivers(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	_, err := e.db.SetResidentTimer("healthy", e.now.Add(time.Minute), "", "", e.now)
	testutil.NoError(t, err)
	e.now = e.now.Add(2 * time.Minute)
	n := &timerNotifier{calls: map[string]string{}, submitted: map[string]bool{}}
	e.m.notifier = func() residentNotifier { return n }
	e.m.fireTimers = func(id string, now time.Time) ([]*model.TaskMessage, error) {
		msgs, err := e.db.FireResidentTimers(id, now)
		testutil.NoError(t, err)
		return msgs, errors.New("another timer failed")
	}
	e.m.deliverTimers()
	testutil.Equal(t, n.callCount, 1)
}

// Exercise the daemon's actual envelope through the real notifier, including
// absent/busy sessions, control-character escaping, dedup, and a daemon bounce.
func TestResidentTimerRealIdleGate(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	res, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	_, err = e.db.SetResidentTimer("check Sam\r\n\x1b[2J", e.now.Add(time.Minute), "", "", e.now)
	testutil.NoError(t, err)
	e.now = e.now.Add(2 * time.Minute)
	r := &fakeNudgeRunner{}
	n := notify.New(r, noFocus{})
	e.m.notifier = func() residentNotifier { return n }
	e.m.deliverTimers()
	msgs, err := e.db.Inbox(res.ID, db.InboxFilter{})
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	id := msgs[0].ID
	n.Reconcile(time.Now())
	testutil.Equal(t, n.DeliveryState(res.ID, id), notify.StatePending)
	sess := r.addSession(res.ID, false)
	n.Reconcile(time.Now())
	testutil.Equal(t, len(sess.writes), 0)
	sess.idle = true
	n.Reconcile(time.Now())
	testutil.Equal(t, n.DeliveryState(res.ID, id), notify.StateSubmitted)
	text := string(bytes.Join(sess.writes, nil))
	testutil.Contains(t, text, "[argus] Timer event "+id)
	testutil.Contains(t, text, `check Sam\r\n\x1b[2J`)
	testutil.False(t, strings.Contains(text, "\x1b[2J"))
	before := len(sess.writes)
	e.m.deliverTimers()
	e.m.deliverTimers()
	n.Reconcile(time.Now())
	testutil.Equal(t, len(sess.writes), before)
	// A fresh notifier has no in-memory receipt. The durable submitted marker
	// must prevent it from registering this still-unread event again.
	n = notify.New(r, noFocus{})
	e.m = newResidentManager(e.db, e.run, func() config.Config { return e.cfg })
	e.m.now = func() time.Time { return e.now }
	e.m.notifier = func() residentNotifier { return n }
	e.m.deliverTimers()
	n.Reconcile(time.Now())
	testutil.Equal(t, len(sess.writes), before)
	testutil.Equal(t, n.DeliveryState(res.ID, id), notify.DeliveryState(""))
}
