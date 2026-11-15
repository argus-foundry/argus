package hera

import (
	"fmt"
	"time"
)

// SetResidentTimers refreshes the resident pane's lightweight readout. UI only.
func (p *HeraPage) SetResidentTimers(count int, next string) {
	p.residentTimerSummary = ""
	if count > 0 {
		noun := "timers"
		if count == 1 {
			noun = "timer"
		}
		p.residentTimerSummary = fmt.Sprintf("%d %s", count, noun)
		if due, err := time.Parse(time.RFC3339, next); err == nil {
			p.residentTimerSummary += " · next " + due.Local().Format("Jan 2 15:04 MST")
		}
	}
	p.applyResidentTitle()
}
