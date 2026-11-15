package daemon

import (
	"fmt"
	"strconv"

	"github.com/argus-foundry/argus/internal/notify"
)

type residentTimerNotifier interface {
	residentNotifier
	Submitted(taskID, deliveryID string) bool
	Cancel(taskID, deliveryID string)
}

// deliverTimers runs only in the daemon. The transaction creates the event
// once; the durable receipt keeps retrying notifier registration after bounces
// and five-minute deadlines, with the same delivery id on every attempt.
func (m *residentManager) deliverTimers() {
	cfg := m.cfgFn()
	if !cfg.Resident.Enabled {
		return
	}
	expression, err := cfg.Resident.MemoryTidySchedule()
	if err != nil {
		residentLog("[resident-memory-tidy] invalid config: %v", err)
		// Fail closed for this built-in, retracting an old queued fire, while
		// ordinary follow-ups continue to work.
		expression = ""
	}
	zone := ""
	if expression != "" {
		resolve := m.memoryTidyZone
		if resolve == nil {
			resolve = residentMemoryTidyZone
		}
		zone, err = resolve()
		if err != nil {
			residentLog("[resident-memory-tidy] disabled: timezone resolution failed: %v", err)
			expression = ""
		}
	}
	if err = m.db.EnsureResidentMemoryTidy(residentMemoryTidyNote(cfg), expression, zone, m.now()); err != nil {
		residentLog("[resident-memory-tidy] reconcile failed: %v", err)
	}
	task, err := m.db.ResidentTask()
	if err != nil || task == nil {
		return
	}
	fired, err := m.db.FireResidentTimers(task.ID, m.now())
	if err != nil {
		residentLog("[resident-timer] fire failed: %v", err)
		return
	}
	for _, msg := range fired {
		residentLog("[resident-timer] fire message=%s task=%s", msg.ID, task.ID)
	}
	pending, err := m.db.PendingResidentTimerDeliveries()
	if err != nil {
		residentLog("[resident-timer] delivery poll failed: %v", err)
		return
	}
	var n residentTimerNotifier
	if m.notifier != nil {
		n, _ = m.notifier().(residentTimerNotifier)
	}
	for _, p := range pending {
		// Match task_message_ack's delivery key so acknowledging the inbox
		// event cancels a queued wake-up immediately through the normal nudger.
		id := p.Message.ID
		if p.Cancelled || !p.Message.ReadAt.IsZero() {
			if n != nil {
				n.Cancel(task.ID, id)
			}
			if err = m.db.CompleteResidentTimerDelivery(p.Message.ID); err != nil {
				residentLog("[resident-timer] cancel receipt failed: %v", err)
			}
			continue
		}
		if n == nil || m.recycling(task.ID) || m.runner.HasPendingRestart(task.ID) {
			residentLog("[resident-timer] skip message=%s resident unavailable", p.Message.ID)
			continue
		}
		if n.Submitted(task.ID, id) {
			if err = m.db.CompleteResidentTimerDelivery(p.Message.ID); err != nil {
				residentLog("[resident-timer] deliver receipt failed: %v", err)
				continue
			}
			residentLog("[resident-timer] deliver message=%s", p.Message.ID)
			continue
		}
		// A notifier payload is one logical terminal line. Keep the original
		// body in the inbox and quote it here so a note's CR/ESC cannot become
		// terminal input, while all note content remains visible to Solo.
		line := fmt.Sprintf("[argus] Timer event %s: %s · read: task_inbox", p.Message.ID, strconv.Quote(p.Message.Body))
		n.ReliableNotify(task.ID, line, id, notify.NotifyOpts{})
	}
}
