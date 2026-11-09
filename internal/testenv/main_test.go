package testenv

import (
	"os"
	"testing"
)

// TestMain scrubs the worker environment before the package's own tests,
// which then set contaminated values deliberately via t.Setenv.
func TestMain(m *testing.M) { os.Exit(Main(m)) }

func TestEnvScrubbed(t *testing.T) { AssertScrubbed(t) }
