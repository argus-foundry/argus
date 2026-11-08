// Package installsigned tests scripts/install-signed.sh against fake `security`
// and `codesign` binaries on PATH. The real keychain / signing identities are
// never touched, and the real script never builds or writes ~/go/bin.
package installsigned

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/drn/argus/internal/testutil"
)

const fakeSecurity = `#!/bin/sh
case "$1" in
find-identity)
  [ "$FAKE_IDENTITY" = "none" ] && exit 0
  echo '  1) ABCDEF0123 "Argus Code Signing"'
  ;;
show-keychain-info)
  [ "$FAKE_KEYCHAIN" = "locked" ] && { echo "User interaction is not allowed." >&2; exit 36; }
  exit 0
  ;;
esac
`

const fakeCodesign = `#!/bin/sh
echo "$@" >> "$FAKE_LOG"
case "$FAKE_CODESIGN" in
hang) exec sleep 30 ;;
fail) exit 1 ;;
esac
for last; do :; done
echo SIGNED >> "$last"
`

const fakeBuild = `#!/bin/sh
echo NEW > "$1"
`

type result struct {
	out     string
	err     error
	dest    string
	log     string
	elapsed time.Duration
}

func run(t *testing.T, env ...string) result {
	t.Helper()
	dir := t.TempDir()
	fb := filepath.Join(dir, "fakebin")
	testutil.NoError(t, os.MkdirAll(fb, 0o755))
	write := func(p, s string) {
		testutil.NoError(t, os.WriteFile(p, []byte(s), 0o755))
	}
	write(filepath.Join(fb, "security"), fakeSecurity)
	write(filepath.Join(fb, "codesign"), fakeCodesign)
	build := filepath.Join(dir, "build.sh")
	write(build, fakeBuild)
	dest := filepath.Join(dir, "bin", "argus")
	testutil.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
	write(dest, "OLD\n")
	logp := filepath.Join(dir, "codesign.log")

	script, err := filepath.Abs("../install-signed.sh")
	testutil.NoError(t, err)
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append([]string{
		"PATH=" + fb + ":/usr/bin:/bin",
		"TMPDIR=" + dir,
		"ARGUS_INSTALL_BIN=" + dest,
		"ARGUS_BUILD_CMD=" + build,
		"ARGUS_SIGN_TIMEOUT=1",
		"FAKE_LOG=" + logp,
	}, env...)
	start := time.Now()
	out, runErr := cmd.CombinedOutput()
	el := time.Since(start)
	b, _ := os.ReadFile(dest)
	lg, _ := os.ReadFile(logp)
	return result{string(out), runErr, string(b), string(lg), el}
}

func TestInstallSigned(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns shell + sleeps")
	}
	t.Run("signs temp then atomically installs", func(t *testing.T) {
		r := run(t)
		testutil.NoError(t, r.err)
		testutil.Contains(t, r.dest, "NEW")
		testutil.Contains(t, r.dest, "SIGNED")
		// codesign must target the temp file, never the live destination.
		testutil.Contains(t, r.log, "argus.new.")
		testutil.Contains(t, r.out, "signed")
	})
	t.Run("locked keychain fails fast, live binary untouched", func(t *testing.T) {
		r := run(t, "FAKE_KEYCHAIN=locked")
		testutil.Equal(t, r.err != nil, true)
		testutil.Equal(t, r.dest, "OLD\n")
		testutil.Equal(t, r.log, "") // codesign never invoked
		testutil.Contains(t, r.out, "keychain is locked")
	})
	t.Run("codesign hang hits timeout, live binary untouched", func(t *testing.T) {
		r := run(t, "FAKE_CODESIGN=hang")
		testutil.Equal(t, r.err != nil, true)
		testutil.Equal(t, r.dest, "OLD\n")
		testutil.Contains(t, r.out, "timed out")
		testutil.Equal(t, r.elapsed < 15*time.Second, true)
	})
	t.Run("codesign failure leaves live binary untouched", func(t *testing.T) {
		r := run(t, "FAKE_CODESIGN=fail")
		testutil.Equal(t, r.err != nil, true)
		testutil.Equal(t, r.dest, "OLD\n")
		testutil.Contains(t, r.out, "codesign failed")
	})
	t.Run("no identity installs unsigned", func(t *testing.T) {
		r := run(t, "FAKE_IDENTITY=none")
		testutil.NoError(t, r.err)
		testutil.Equal(t, r.dest, "NEW\n")
		testutil.Equal(t, r.log, "")
	})
	t.Run("skip sign installs unsigned even when locked", func(t *testing.T) {
		r := run(t, "ARGUS_SKIP_SIGN=1", "FAKE_KEYCHAIN=locked")
		testutil.NoError(t, r.err)
		testutil.Equal(t, r.dest, "NEW\n")
	})
	t.Run("build failure leaves live binary untouched", func(t *testing.T) {
		r := run(t, "ARGUS_BUILD_CMD=/usr/bin/false")
		testutil.Equal(t, r.err != nil, true)
		testutil.Equal(t, r.dest, "OLD\n")
	})
}
