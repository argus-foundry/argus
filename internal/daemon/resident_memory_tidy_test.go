package daemon

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentMemoryTidyDelivery(t *testing.T) {
	e := newResEnv(t)
	e.cfg.Resident.MemoryTidy = "02:30"
	e.cfg.Resident.MemoryDir = "solo/memory"
	n := &timerNotifier{calls: map[string]string{}, submitted: map[string]bool{}}
	e.m.notifier = func() residentNotifier { return n }
	e.m.tick()
	e.m.deliverTimers()
	list, err := e.db.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 1)
	due := list[0].DueAt
	e.now = due.Add(time.Minute)
	e.m.deliverTimers()
	res, err := e.db.ResidentTask()
	testutil.NoError(t, err)
	inbox, err := e.db.Inbox(res.ID, db.InboxFilter{})
	testutil.NoError(t, err)
	testutil.Equal(t, len(inbox), 1)
	testutil.Equal(t, len(n.calls), 1)
	for _, s := range []string{"solo/memory/", "Merge duplicate", "newest dated evidence", "resident_timer_cancel", "Keep files lean", "Fix broken links", "NEVER delete Aaron's explicit instructions or preferences", "short dated change log", "Stay quiet", "dream: skip"} {
		testutil.Contains(t, inbox[0].Body, s)
	}
	e.cfg.Resident.MemoryTidy = "off"
	e.m.deliverTimers()
	testutil.Equal(t, len(n.calls), 0)
	testutil.Equal(t, len(n.cancelled), 1)
	list, err = e.db.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 0)
	inbox, err = e.db.Inbox(res.ID, db.InboxFilter{})
	testutil.NoError(t, err)
	testutil.Equal(t, len(inbox), 1)
	testutil.False(t, inbox[0].ReadAt.IsZero())
}

func TestResidentMemoryTidyInvalidConfig(t *testing.T) {
	e := newResEnv(t)
	e.m.tick()
	n := &timerNotifier{calls: map[string]string{}, submitted: map[string]bool{}}
	e.m.notifier = func() residentNotifier { return n }
	e.cfg.Resident.MemoryTidy = "02:30"
	e.m.deliverTimers()
	list, err := e.db.ResidentTimers()
	testutil.NoError(t, err)
	e.now = list[0].DueAt.Add(time.Minute)
	e.m.deliverTimers()
	testutil.Equal(t, len(n.calls), 1)
	_, err = e.db.SetResidentTimer("personal follow-up", e.now.Add(time.Minute), "", "", e.now)
	testutil.NoError(t, err)
	e.now = e.now.Add(2 * time.Minute)
	e.cfg.Resident.MemoryTidy = "invalid"
	e.m.deliverTimers()
	list, err = e.db.ResidentTimers()
	testutil.NoError(t, err)
	testutil.Equal(t, len(list), 0)
	testutil.Equal(t, len(n.cancelled), 1)
	testutil.Equal(t, len(n.calls), 1)
	for _, body := range n.calls {
		testutil.Contains(t, body, "personal follow-up")
	}
}
