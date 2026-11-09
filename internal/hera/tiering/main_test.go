package tiering

import (
	"os"
	"testing"

	"github.com/argus-foundry/argus/internal/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }

func TestEnvScrubbed(t *testing.T) { testenv.AssertScrubbed(t) }
