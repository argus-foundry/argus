package config

import (
	"fmt"
	"time"
)

// MemoryTidySchedule returns the built-in timer's cron expression; empty means off.
func (r ResidentConfig) MemoryTidySchedule() (string, error) {
	value := r.MemoryTidy
	if value == "off" {
		return "", nil
	}
	if value == "" {
		value = "02:30"
	}
	clock, err := time.Parse("15:04", value)
	if err != nil || clock.Format("15:04") != value {
		return "", fmt.Errorf("resident.memory_tidy must be HH:MM local time or off")
	}
	return fmt.Sprintf("%d %d * * *", clock.Minute(), clock.Hour()), nil
}
