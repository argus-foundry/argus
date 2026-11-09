package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTestServer_IsolatesHome(t *testing.T) {
	inheritedHome := t.TempDir()
	t.Setenv("HOME", inheritedHome)
	srv, _ := testServer(t)
	testutil.True(t, os.Getenv("HOME") != inheritedHome)
	testutil.Equal(t, db.DataDir(), filepath.Join(os.Getenv("HOME"), ".argus"))
	testutil.Equal(t, len(getHera(t, srv).Orchestrators), 0)
}
