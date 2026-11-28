package voice

import (
	"os"
	"testing"

	"github.com/argus-foundry/argus/internal/testenv"
)

// TestMain scrubs agent/host environment variables so results match CI.
func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

func TestEnvScrubbed(t *testing.T) { testenv.AssertScrubbed(t) }
