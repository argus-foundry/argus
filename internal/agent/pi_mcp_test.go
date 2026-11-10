package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/backendkind"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestPiMCPRegistration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fam := FamilyForKind(backendkind.Pi)
	for _, launching := range []bool{false, true} {
		t.Run(map[bool]string{false: "startup", true: "launch"}[launching], func(t *testing.T) {
			testutil.NoError(t, fam.InjectGlobal(7742, launching))
			data, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "mcp.json"))
			testutil.NoError(t, err)
			testutil.Contains(t, string(data), "http://localhost:7742/mcp")
		})
	}
}
