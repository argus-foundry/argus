package daemon

import (
	"fmt"
	"strconv"

	"github.com/argus-foundry/argus/internal/notify"
)

type residentTimerNotifier interface {
	residentNotifier
	DeliveryState(taskID, deliveryID string) notify.DeliveryState
	Cancel(taskID, deliveryID string)
}

// deliverTimers runs only in the daemon. The transaction creates the event
// once; the durable receipt keeps retrying notifier registration after bounces
// and five-minute deadlines, with the same delivery id on every attempt.
func (m *residentManager) deliverTimers() {
	if !m.cfgFn().Resident.Enabled {
		return
	}
	task, err := m.db.ResidentTask()
	if err != nil || task == nil {
		return
	}
	m.deliverPendingTimers(task.ID)
	fire := m.fireTimers
	if fire == nil {
		fire = m.db.FireResidentTimers
	}
	fired, err := fire(task.ID, m.now())
	if err != nil {
		residentLog("[resident-timer] fire failed: %v", err)
	}
	for _, msg := range fired {
		residentLog("[resident-timer] fire message=%s task=%s", msg.ID, task.ID)
	}
	m.deliverPendingTimers(task.ID)
	if count, err := m.db.PruneResidentTimers(m.now()); err != nil {
		residentLog("[resident-timer] prune failed: %v", err)
	} else if count > 0 {
		residentLog("[resident-timer] skip pruned terminal timers count=%d", count)
	}
}

func (m *residentManager) deliverPendingTimers(taskID string) {
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
		if p.Cancelled || p.Orphan || !p.Message.ReadAt.IsZero() {
			if n != nil {
				n.Cancel(taskID, id)
			}
			if err = m.db.CompleteResidentTimerDelivery(p.Message.ID); err != nil {
				residentLog("[resident-timer] cancel receipt failed: %v", err)
			}
			continue
		}
		if p.Submitted {
			continue
		}
		if n == nil || m.recycling(taskID) || m.runner.HasPendingRestart(taskID) {
			residentLog("[resident-timer] skip message=%s resident unavailable", p.Message.ID)
			continue
		}
		if n.DeliveryState(taskID, id) == notify.StateSubmitted {
			if err = m.db.MarkResidentTimerDelivered(p.Message.ID); err != nil {
				residentLog("[resident-timer] deliver receipt failed: %v", err)
				continue
			}
			residentLog("[resident-timer] deliver message=%s", p.Message.ID)
			continue
		}
		if n.DeliveryState(taskID, id) == notify.StatePending {
			continue
		}
		// A notifier payload is one logical terminal line. Keep the original
		// body in the inbox and quote it here so a note's CR/ESC cannot become
		// terminal input, while all note content remains visible to Solo.
		line := fmt.Sprintf("[argus] Timer event %s: %s · read: task_inbox", p.Message.ID, strconv.Quote(p.Message.Body))
		n.ReliableNotify(taskID, line, id, notify.NotifyOpts{})
	}
}
