package db

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

// BenchmarkOpenInMemory measures the schema cost paid by each fresh test DB.
func BenchmarkOpenInMemory(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	for b.Loop() {
		d, err := OpenInMemory()
		testutil.NoError(b, err)
		testutil.NoError(b, d.Close())
	}
}
