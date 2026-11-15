package hera

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentTimerTitle(t *testing.T) {
	f := newResidentFixture(t)
	f.enter(t)
	f.p.SetResidentTitle("Solo")
	f.p.SetResidentTimers(2, "2026-11-14T15:00:00Z")
	testutil.Contains(t, f.p.coordTitle, "Solo · 2 timers · next")
	f.p.SetResidentTitle("Solo") // periodic label refresh preserves summary
	testutil.Contains(t, f.p.coordTitle, "2 timers")
	f.p.SetResidentTimers(0, "")
	testutil.Equal(t, f.p.coordTitle, "Solo")
	f.p.SetResidentTimers(1, "bad")
	testutil.Equal(t, f.p.coordTitle, "Solo · 1 timer")
}
