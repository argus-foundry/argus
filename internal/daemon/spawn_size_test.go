package daemon

import (
	"testing"
	"time"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/testutil"
)

type ptySizer struct{ cols, rows int }

func (p ptySizer) PTYSize() (int, int) { return p.cols, p.rows }

func publishLayout(t *testing.T, d *db.DB, at time.Time) {
	t.Helper()
	testutil.NoError(t, d.PublishInitialPTYLayout(db.InitialPTYLayout{
		Active:      db.PTYSize{Rows: 40, Cols: 140},
		Coordinator: db.PTYSize{Rows: 45, Cols: 150},
		Worker:      db.PTYSize{Rows: 35, Cols: 100},
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
