package sysmetrics

import (
	"os"
	"testing"

	"github.com/drn/argus/internal/testenv"
)

// TestMain scrubs agent/host environment variables (ARGUS_*, CODEX_*,
// CLAUDE_*) so results match CI inside a worker sandbox.
func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

func TestEnvScrubbed(t *testing.T) { testenv.AssertScrubbed(t) }
