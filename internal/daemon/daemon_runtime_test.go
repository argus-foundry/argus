package daemon

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/api"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

type mountedRuntime struct{}

func (mountedRuntime) Snapshot() api.RuntimeSnapshot {
	return api.RuntimeSnapshot{
		DaemonHash: "mounted-daemon", DaemonBootedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		DaemonState: "stale", InstalledHash: "mounted-installed", SupervisorHash: "mounted-supervisor",
		SupervisorState: "current", SupervisorCompatibility: "compatible", RestartSupported: true,
	}
}

func TestDaemonRuntimeMountedAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, sock := testDaemon(t)
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	testutil.NoError(t, err)
	requestedPort := reservation.Addr().(*net.TCPAddr).Port
	testutil.NoError(t, reservation.Close())
	for key, value := range map[string]string{"api.enabled": "true", "api.http_port": strconv.Itoa(requestedPort), "kb.enabled": "false", "resident.enabled": "false"} {
		testutil.NoError(t, d.db.SetConfigValue(key, value))
	}
	d.SetRuntimeSource(mountedRuntime{})
	serveTestServer(t, d, sock)
	waitForSocket(t, sock)
	var pong PongResp
	testutil.NoError(t, dialRPC(t, sock).Call("Daemon.Ping", &Empty{}, &pong))
	d.mu.Lock()
	port := d.apiPort
	d.mu.Unlock()
	testutil.True(t, port > 0)
	token, err := os.ReadFile(filepath.Join(db.DataDir(), "api-token"))
	testutil.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/runtime", nil)
	testutil.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	testutil.NoError(t, err)
	defer resp.Body.Close()
	testutil.Equal(t, resp.StatusCode, http.StatusOK)
	var out map[string]any
	testutil.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	bootID, ok := out["boot_id"].(string)
	testutil.True(t, ok)
	testutil.True(t, bootID != "")
	delete(out, "boot_id")
	testutil.DeepEqual(t, out, map[string]any{
		"daemon":            map[string]any{"hash": "mounted-daemon", "booted_at": "2026-01-02T03:04:05Z", "state": "stale"},
		"installed":         map[string]any{"hash": "mounted-installed"},
		"supervisor":        map[string]any{"hash": "mounted-supervisor", "state": "current", "compatibility": "compatible"},
		"restart_supported": true,
	})
}
