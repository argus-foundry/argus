package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/argus-foundry/argus/internal/uxlog"
)

// RuntimeSnapshot is the identity view /api/runtime serves. Empty hash strings
// mean "unknown" and serialize as null; a missing identity is never inferred stale.
type RuntimeSnapshot struct {
	DaemonHash              string
	DaemonBootedAt          time.Time
	DaemonState             string
	InstalledHash           string
	SupervisorHash          string
	SupervisorState         string
	SupervisorCompatibility string
	RestartSupported        bool
}

// RuntimeSource supplies a fresh snapshot per request. The daemon wires an
// implementation backed by BootInfo and the shared binary-coherence policy.
type RuntimeSource interface {
	Snapshot() RuntimeSnapshot
}

// SetRuntimeSource wires the source behind /api/runtime and /api/daemon/restart.
func (s *Server) SetRuntimeSource(src RuntimeSource) { s.runtimeSource = src }

func newBootID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b)
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func (s *Server) snapshot() RuntimeSnapshot {
	if s.runtimeSource == nil {
		return RuntimeSnapshot{}
	}
	return s.runtimeSource.Snapshot()
}

func (s *Server) handleRuntime(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot()
	var booted *string
	if !snap.DaemonBootedAt.IsZero() {
		v := snap.DaemonBootedAt.UTC().Format(time.RFC3339)
		booted = &v
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"boot_id": s.bootID,
		"daemon": map[string]any{
			"hash":      nullable(snap.DaemonHash),
			"booted_at": booted,
			"state":     orUnknown(snap.DaemonState),
		},
		"installed": map[string]any{"hash": nullable(snap.InstalledHash)},
		"supervisor": map[string]any{
			"hash":          nullable(snap.SupervisorHash),
			"state":         orUnknown(snap.SupervisorState),
			"compatibility": orUnknown(snap.SupervisorCompatibility),
		},
		"restart_supported": snap.RestartSupported,
	})
}

type daemonRestartReq struct {
	ExpectedBootID string `json:"expected_boot_id"`
}

// restartState allows one successor at a time until it takes over or exits.
type restartState struct{ pending atomic.Bool }

// handleDaemonRestart replaces only the daemon process with the installed binary.
// It never builds, never touches the supervisor, and flushes 202 before handoff.
func (s *Server) handleDaemonRestart(w http.ResponseWriter, r *http.Request) {
	if requireMaster(w, r) {
		return
	}
	var req daemonRestartReq
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.ExpectedBootID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected_boot_id required"})
		return
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected_boot_id required"})
		return
	}
	if !s.snapshot().RestartSupported {
		uxlog.Log("[api] daemon restart refused: unsupported host mode")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "restart_unsupported"})
		return
	}
	if req.ExpectedBootID != s.bootID {
		uxlog.Log("[api] daemon restart refused: stale boot id")
		writeJSON(w, http.StatusConflict, map[string]string{"error": "boot_id_changed", "boot_id": s.bootID})
		return
	}
	if !s.restart.pending.CompareAndSwap(false, true) {
		uxlog.Log("[api] daemon restart refused: already pending")
		writeJSON(w, http.StatusConflict, map[string]string{"error": "restart_pending", "boot_id": s.bootID})
		return
	}
	preflight := s.restartPreflight
	if preflight == nil {
		preflight = defaultRestartPreflight
	}
	if err := preflight(); err != nil {
		s.restart.pending.Store(false)
		uxlog.Log("[api] daemon restart not accepted: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "restart_failed"})
		return
	}
	uxlog.Log("[api] daemon restart accepted")
	writeJSON(w, http.StatusAccepted, map[string]any{"restart_requested": true, "boot_id": s.bootID})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	spawn := s.restartSpawn
	if spawn == nil {
		spawn = spawnRestartSuccessor
	}
	delay := s.restartDelay
	go func() {
		time.Sleep(delay)
		wait, err := spawn()
		if err != nil {
			uxlog.Log("[api] daemon restart: successor launch failed: %v", err)
			slog.Error("[api] daemon restart: successor launch failed", "err", err)
		} else {
			err = wait()
			uxlog.Log("[api] daemon restart: successor exited before takeover: %v", err)
			slog.Warn("[api] daemon restart: successor exited before takeover", "err", err)
		}
		s.restart.pending.Store(false)
	}()
}

func defaultRestartPreflight() error {
	if isTestBinary() {
		return errSpawnFromTestBinary
	}
	return nil
}

// Keep ownership until exit so an exec that fails during startup permits retry.
func startRestartSuccessor(cmd *exec.Cmd) (func() error, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Wait, nil
}

func spawnRestartSuccessor() (func() error, error) {
	if isTestBinary() {
		return nil, errSpawnFromTestBinary
	}
	cmd, err := successorDaemonCommand()
	if err != nil {
		return nil, err
	}
	return startRestartSuccessor(cmd)
}
