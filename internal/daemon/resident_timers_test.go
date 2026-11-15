package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/notify"
	"github.com/argus-foundry/argus/internal/testutil"
)

type timerNotifier struct {
	calls     map[string]string
	submitted map[string]bool
	cancelled []string
	onNotify  func()
}

func (n *timerNotifier) ReliableNotify(task, text, id string, _ notify.NotifyOpts) func() {
	n.calls[id] = text
	if n.onNotify != nil {
		n.onNotify()
	}
	return func() {}
}

func TestResidentTimerRunLoopWiring(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	_, err := e.db.SetResidentTimer("run loop", e.now.Add(time.Minute), "", "", e.now)
	testutil.NoError(t, err)
	e.now = e.now.Add(2 * time.Minute)
	done := make(chan struct{})
	n := &timerNotifier{calls: map[string]string{}, submitted: map[string]bool{}, onNotify: func() { close(done) }}
	e.m.notifier = func() residentNotifier { return n }
	e.m.run(done)
	testutil.Equal(t, len(n.calls), 1)
}
func (n *timerNotifier) Submitted(task, id string) bool { return n.submitted[id] }
func (n *timerNotifier) Cancel(task, id string) {
	n.cancelled = append(n.cancelled, id)
	delete(n.calls, id)
}

func TestResidentTimerDelivery(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	res, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	_, err = e.db.SetResidentTimer("check reply\r\n\x1b[2J", e.now.Add(time.Minute), "", "", e.now)
	testutil.NoError(t, err)
	e.now = e.now.Add(2 * time.Minute)
	// A fire survives an unavailable notifier and a reconstructed manager.
	e.m.deliverTimers()
	msgs, err := e.db.Inbox(res.ID, db.InboxFilter{})
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
	n := &timerNotifier{calls: map[string]string{}, submitted: map[string]bool{}}
	e.m = newResidentManager(e.db, e.run, func() config.Config { return e.cfg })
	e.m.now = func() time.Time { return e.now }
	e.m.notifier = func() residentNotifier { return n }
	e.m.recycling = func(string) bool { return true }
	e.m.deliverTimers()
	testutil.Equal(t, len(n.calls), 0)
	e.m.recycling = func(string) bool { return false }
	e.m.deliverTimers()
	e.m.deliverTimers()
	testutil.Equal(t, len(n.calls), 1)
	id := msgs[0].ID
	testutil.Contains(t, n.calls[id], "[argus] Timer")
	testutil.Contains(t, n.calls[id], "check reply")
	testutil.False(t, strings.ContainsAny(n.calls[id], "\r\n\x1b"))
	n.submitted[id] = true
	e.m.deliverTimers()
	pending, err := e.db.PendingResidentTimerDeliveries()
	testutil.NoError(t, err)
	testutil.Equal(t, len(pending), 0)
	msgs, err = e.db.Inbox(res.ID, db.InboxFilter{})
	testutil.NoError(t, err)
	testutil.Equal(t, len(msgs), 1)
}

func TestResidentTimerCancelAndAck(t *testing.T) {
	for _, ack := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "ack"}[ack], func(t *testing.T) {
			e := newResEnv(t)
			e.m.tick()
			n := &timerNotifier{calls: map[string]string{}, submitted: map[string]bool{}}
			e.m.notifier = func() residentNotifier { return n }
			timer, err := e.db.SetResidentTimer("check", e.now.Add(time.Minute), "", "", e.now)
			testutil.NoError(t, err)
			e.now = e.now.Add(2 * time.Minute)
			e.m.deliverTimers()
			pending, err := e.db.PendingResidentTimerDeliveries()
			testutil.NoError(t, err)
			testutil.Equal(t, len(pending), 1)
			if ack {
				_, err = e.db.AckMessages(pending[0].Message.To, []string{pending[0].Message.ID})
			} else {
				err = e.db.CancelResidentTimer(timer.ID)
			}
			testutil.NoError(t, err)
			e.m.deliverTimers()
			testutil.Equal(t, len(n.cancelled), 1)
			pending, err = e.db.PendingResidentTimerDeliveries()
			testutil.NoError(t, err)
			testutil.Equal(t, len(pending), 0)
		})
	}
}

func TestResidentTimerDisabledAndState(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	_, err := e.db.SetResidentTimer("check", e.now.Add(time.Minute), "", "", e.now)
	testutil.NoError(t, err)
	st := e.m.State()
	testutil.Equal(t, st.TimerCount, 1)
	testutil.Equal(t, st.NextTimerAt, e.now.Add(time.Minute).UTC().Format(time.RFC3339))
	e.cfg.Resident.Enabled = false
	e.now = e.now.Add(time.Hour)
	e.m.deliverTimers()
	list, err := e.db.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 1)
}
