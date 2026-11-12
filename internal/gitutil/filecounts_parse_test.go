package gitutil

import (
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestParseZ(t *testing.T) {
	t.Run("numstat rename and binary", func(t *testing.T) {
		m := parseNumstatZ("1\t2\ta b.txt\x00-\t-\tbin\x003\t0\t\x00old\x00new\x00")
		testutil.Equal(t, m["a b.txt"].add, 1)
		testutil.Equal(t, m["a b.txt"].del, 2)
		testutil.Equal(t, m["bin"].binary, true)
		testutil.Equal(t, m["new"].orig, "old")
	})
	t.Run("name-status", func(t *testing.T) {
		f := parseNameStatusZ("M\x00a\x00R100\x00o\x00n\x00D\x00z\x00")
		testutil.Equal(t, len(f), 3)
		testutil.Equal(t, f[1].OriginalPath, "o")
		testutil.Equal(t, f[1].Path, "n")
		testutil.Equal(t, f[2].Status, "D")
	})
	t.Run("status collapse", func(t *testing.T) {
		f := parseStatusZ(" M a\x00A  b\x00?? c\x00UU d\x00R  n\x00o\x00")
		var got []string
		for _, x := range f {
			got = append(got, x.Status)
		}
		testutil.DeepEqual(t, got, []string{"M", "A", "??", "U", "R"})
		testutil.Equal(t, f[4].OriginalPath, "o")
	})
}
