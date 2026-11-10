package daemon

import (
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

type ptySizer struct{ cols, rows int }

func (p ptySizer) PTYSize() (int, int) { return p.cols, p.rows }

func publishLayout(t *testing.T, d *db.DB, at time.Time) {
	t.Helper()
	testutil.NoError(t, d.PublishInitialPTYLayout(db.InitialPTYLayout{
		Active:      db.PTYSize{Rows: 40, Cols: 140},
		Coordinator: db.PTYSize{Rows: 45, Cols: 150},
		Worker:      db.PTYSize{Rows: 35, Cols: 100},
		Resident:    db.PTYSize{Rows: 50, Cols: 238},
		ReportedAt:  at,
	}))
}

// resize-without-restart: a restart spawns at the fresh viewer-reported pane
// size for its role, then the live PTY size, then 24x80 — never a stale
// outgoing PTY when a fresher report exists.
func TestResolveSpawnSize(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		report     bool
		reportAge  time.Duration
		role       db.HeraRoleKind
		sess       interface{ PTYSize() (int, int) }
		wantRows   uint16
		wantCols   uint16
		wantSource string
	}{
		{"fresh coordinator report beats stale pty", true, 0, db.HeraKindCoordinator, ptySizer{80, 24}, 45, 150, "report"},
		{"worker role uses worker pane", true, 0, db.HeraKindWorker, ptySizer{80, 24}, 35, 100, "report"},
		{"resident uses its own pane, not active", true, 0, db.InitialPTYRoleResident, ptySizer{80, 24}, 50, 238, "report"},
		{"plain task uses active pane", true, 0, "", ptySizer{80, 24}, 40, 140, "report"},
		{"expired report falls back to live pty", true, db.InitialPTYReportTTL + time.Minute, db.HeraKindCoordinator, ptySizer{120, 40}, 40, 120, "pty"},
		{"no report uses live pty", false, 0, db.HeraKindCoordinator, ptySizer{120, 40}, 40, 120, "pty"},
		{"no session and no report defaults", false, 0, db.HeraKindCoordinator, nil, 24, 80, "default"},
		{"zero pty size is ignored", false, 0, "", ptySizer{0, 0}, 24, 80, "default"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			d, err := db.OpenInMemory()
			testutil.NoError(t, err)
			t.Cleanup(func() { _ = d.Close() })
			if tc.report {
				publishLayout(t, d, now.Add(-tc.reportAge))
			}
			rows, cols, source := resolveSpawnSize(d, tc.role, tc.sess, now)
			testutil.Equal(t, rows, tc.wantRows)
			testutil.Equal(t, cols, tc.wantCols)
			testutil.Equal(t, source, tc.wantSource)
		})
	}
}

// An older TUI publishes no resident size: the resident must fall back to the
// live PTY then 24x80, never to Active (whatever view the user happens to be on).
func TestResolveSpawnSize_ResidentWithoutReportedSize(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	now := time.Now()
	testutil.NoError(t, d.PublishInitialPTYLayout(db.InitialPTYLayout{
		Active: db.PTYSize{Rows: 40, Cols: 140}, ReportedAt: now,
	}))
	rows, cols, source := resolveSpawnSize(d, db.InitialPTYRoleResident, ptySizer{120, 40}, now)
	testutil.Equal(t, [3]any{rows, cols, source}, [3]any{uint16(40), uint16(120), "pty"})
	rows, cols, source = resolveSpawnSize(d, db.InitialPTYRoleResident, nil, now)
	testutil.Equal(t, [3]any{rows, cols, source}, [3]any{uint16(24), uint16(80), "default"})
}
