package agent

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/usagebudget"
)

func planCfg(accounts ...string) config.Config {
	cfg := config.DefaultConfig()
	cfg.Accounts = map[string]config.Account{
		"personal": {ClaudeConfigDir: "/tmp/p"},
		"work":     {ClaudeConfigDir: "/tmp/w"},
		"cx":       {CodexHome: "/tmp/cx"},
	}
	cfg.Resident.Accounts = accounts
	return cfg
}

func rd(five, week float64, now time.Time) usagebudget.Reading {
	r := usagebudget.Reading{Percentage: max(five, week)}
	r.FiveHour = &usagebudget.Window{Percentage: five, ResetAt: now.Add(time.Hour), LastProbedAt: now}
	r.SevenDay = &usagebudget.Window{Percentage: week, ResetAt: now.Add(48 * time.Hour), LastProbedAt: now}
	return r
}

func TestResidentAccountEntries(t *testing.T) {
	tests := []struct {
		name      string
		list      []string
		want      []string
		wantNotes int
	}{
		{"unset is default", nil, []string{"default"}, 0},
		{"order kept", []string{"personal", "default", "work"}, []string{"personal", "default", "work"}, 0},
		{"auto alone", []string{"auto"}, []string{"auto"}, 0},
		{"auto last", []string{"personal", "auto"}, []string{"personal", "auto"}, 0},
		{"after auto dropped", []string{"auto", "personal"}, []string{"auto"}, 1},
		{"unknown and codex-only skipped", []string{"ghost", "cx", "work"}, []string{"work"}, 2},
		{"duplicate skipped", []string{"work", "work"}, []string{"work"}, 1},
		{"blank skipped", []string{" ", "work"}, []string{"work"}, 0},
		{"nothing usable falls back", []string{"ghost"}, []string{"default"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, notes := ResidentAccountEntries(planCfg(tt.list...))
			testutil.DeepEqual(t, got, tt.want)
			testutil.Equal(t, len(notes), tt.wantNotes)
		})
	}
}

func TestResidentStartAccount(t *testing.T) {
	now := time.Now()
	full := rd(100, 10, now)
	weekFull := rd(0, 100, now)
	ok := rd(20, 20, now)
	tests := []struct {
		name   string
		list   []string
		cur    string
		manual bool
		usage  map[string]usagebudget.Reading
		want   string
	}{
		{"fresh start first listed", []string{"personal", "work"}, "", false, nil, "personal"},
		{"fresh start skips depleted", []string{"personal", "work"}, "", false, map[string]usagebudget.Reading{"personal": full}, "work"},
		{"weekly alone depletes", []string{"personal", "work"}, "", false, map[string]usagebudget.Reading{"personal": weekFull}, "work"},
		{"keeps allowed current", []string{"personal", "work"}, "work", false, map[string]usagebudget.Reading{"personal": ok}, "work"},
		{"current depleted moves on", []string{"personal", "work"}, "personal", false, map[string]usagebudget.Reading{"personal": full}, "work"},
		{"current not in list, auto marker", []string{"work"}, "personal", false, nil, "work"},
		{"manual out of list kept", []string{"work"}, "personal", true, nil, "personal"},
		{"manual depleted repicked", []string{"work"}, "personal", true, map[string]usagebudget.Reading{"personal": full}, "work"},
		{"auto entry picks most headroom", []string{"personal", "auto"}, "", false, map[string]usagebudget.Reading{"personal": full, "default": rd(50, 50, now), "work": rd(5, 5, now)}, "work"},
		{"auto covers unlisted current", []string{"personal", "auto"}, "work", false, map[string]usagebudget.Reading{"personal": full}, "work"},
		{"all depleted keeps current", []string{"personal", "work"}, "work", false, map[string]usagebudget.Reading{"personal": full, "work": full}, "work"},
		{"all depleted first start uses first listed", []string{"personal", "work"}, "", false, map[string]usagebudget.Reading{"personal": full, "work": full}, "personal"},
		{"default list", nil, "", false, nil, "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := ResidentStartAccount(planCfg(tt.list...), tt.cur, tt.manual, tt.usage, now)
			testutil.Equal(t, got, tt.want)
		})
	}
}

func TestDecideResidentAccount(t *testing.T) {
	now := time.Now()
	full := rd(100, 10, now)
	weekFull := rd(0, 100, now)
	tests := []struct {
		name       string
		list       []string
		cur        string
		manual     bool
		usage      map[string]usagebudget.Reading
		wantTarget string
		wantReason string
		wantNone   bool
	}{
		{"depleted goes to next listed not lowest usage", []string{"personal", "work", "auto"}, "personal", false,
			map[string]usagebudget.Reading{"personal": weekFull, "work": rd(90, 90, now), "default": rd(1, 1, now)}, "work", ResidentMoveDepleted, false},
		{"depleted manual still moves", []string{"personal", "work"}, "personal", true,
			map[string]usagebudget.Reading{"personal": full}, "work", ResidentMoveDepleted, false},
		{"every listed depleted", []string{"personal", "work"}, "personal", false,
			map[string]usagebudget.Reading{"personal": full, "work": full}, "", "", true},
		{"healthy stays", []string{"personal", "work"}, "personal", false, nil, "", "", false},
		{"moves back up on a fresh reading", []string{"personal", "work"}, "work", false,
			map[string]usagebudget.Reading{"personal": rd(5, 5, now)}, "personal", ResidentMoveBackUp, false},
		{"no move back up on an unknown reading", []string{"personal", "work"}, "work", false, nil, "", "", false},
		{"no move back up on a stale reading", []string{"personal", "work"}, "work", false,
			map[string]usagebudget.Reading{"personal": rd(5, 5, now.Add(-3*time.Hour))}, "", "", false},
		{"manual not moved back up", []string{"personal", "work"}, "work", true, nil, "", "", false},
		{"no move back up while preferred depleted", []string{"personal", "work"}, "work", false,
			map[string]usagebudget.Reading{"personal": full}, "", "", false},
		{"auto slot does not chase headroom", []string{"personal", "auto"}, "work", false,
			map[string]usagebudget.Reading{"personal": full, "work": rd(80, 80, now), "default": rd(1, 1, now)}, "", "", false},
		{"auto slot returns to explicit", []string{"personal", "auto"}, "work", false,
			map[string]usagebudget.Reading{"personal": rd(5, 5, now)}, "personal", ResidentMoveBackUp, false},
		{"not allowed moves onto list (unknown ok when leaving)", []string{"personal"}, "work", false, nil, "personal", ResidentMoveNotAllowed, false},
		{"manual out of list stays", []string{"personal"}, "work", true, nil, "", "", false},
		{"unknown reading never depleted", []string{"personal", "work"}, "personal", false, map[string]usagebudget.Reading{}, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := DecideResidentAccount(planCfg(tt.list...), tt.cur, tt.manual, tt.usage, now)
			testutil.Equal(t, d.Target, tt.wantTarget)
			testutil.Equal(t, d.Reason, tt.wantReason)
			testutil.Equal(t, d.NoneUsable, tt.wantNone)
			testutil.Equal(t, d.Urgent, tt.wantReason == ResidentMoveDepleted)
		})
	}
}
