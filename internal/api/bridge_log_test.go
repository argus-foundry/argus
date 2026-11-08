package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/drn/argus/internal/testutil"
)

type bridgeWarningProbe struct {
	slog.Handler
	ingest   *sync.Mutex
	mu       sync.Mutex
	warnings []slog.Record
	lockHeld bool
}

func (p *bridgeWarningProbe) Handle(_ context.Context, r slog.Record) error {
	if !strings.HasPrefix(r.Message, "[bridge] invalid measurement") {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.warnings = append(p.warnings, r.Clone())
	if p.ingest.TryLock() {
		p.ingest.Unlock()
	} else {
		p.lockHeld = true
	}
	return nil
}

func TestBridgeInvalidMeasurementLogsBoundedSummary(t *testing.T) {
	s, _, task := bridgeAuthorityServer(t)
	probe := &bridgeWarningProbe{Handler: slog.NewTextHandler(io.Discard, nil), ingest: &s.bridgeIngestMu}
	previous := slog.Default()
	slog.SetDefault(slog.New(probe))
	t.Cleanup(func() { slog.SetDefault(previous) })
	const invalid = `{"type":"session.measure","context":{"tokens":-1}}`
	body := `{"events":[` + strings.Repeat(invalid+",", 100) + `{"type":"turn.start","generation":"launch","session_id":"session","boot":"b","seq":1}]}`
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, authedReq("POST", "/api/tasks/"+task.ID+"/bridge/events", body))
	testutil.Equal(t, w.Code, 200)
	var counts map[string]int
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &counts))
	testutil.Equal(t, counts["accepted"], 1)
	testutil.Equal(t, counts["received"], 101)
	testutil.True(t, s.BridgeStates()[task.ID].Busy)
	probe.mu.Lock()
	defer probe.mu.Unlock()
	testutil.Equal(t, len(probe.warnings), 1)
	testutil.False(t, probe.lockHeld)
	if len(probe.warnings) != 1 {
		return
	}
	attrs := make(map[string]any)
	probe.warnings[0].Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value.Any(); return true })
	testutil.Equal(t, attrs["task"], any(task.ID))
	testutil.Equal(t, attrs["invalid"], any(int64(100)))
	testutil.Equal(t, attrs["err"], any("context tokens must not be negative"))
}
