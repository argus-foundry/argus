// Package installsigned tests scripts/install-signed.sh against fake `security`
// and `codesign` binaries on PATH. The real keychain / signing identities are
// never touched, and the real script never builds or writes ~/go/bin.
package installsigned

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
echo $$ > "$FAKE_PIDFILE"
case "$FAKE_CODESIGN" in
hang) exec sleep 600 ;;
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

type fixture struct {
	dir, dest, logp, pidp string
	cmd                   *exec.Cmd
}

func newFixture(t *testing.T, env ...string) *fixture {
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
	f := &fixture{dir: dir, dest: filepath.Join(dir, "bin", "argus"),
		logp: filepath.Join(dir, "codesign.log"), pidp: filepath.Join(dir, "codesign.pid")}
	testutil.NoError(t, os.MkdirAll(filepath.Dir(f.dest), 0o755))
	write(f.dest, "OLD\n")
	script, err := filepath.Abs("../install-signed.sh")
	testutil.NoError(t, err)
	f.cmd = exec.Command("/bin/sh", script)
	f.cmd.Env = append([]string{
		"PATH=" + fb + ":/usr/bin:/bin",
		"TMPDIR=" + dir,
		"ARGUS_INSTALL_BIN=" + f.dest,
		"ARGUS_BUILD_CMD=" + build,
		// Generous: the fake security probes are instant, but a loaded CI host can
		// take seconds to spawn them. Tests that need codesign to time out set
		// ARGUS_CODESIGN_TIMEOUT instead of shrinking this.
		"ARGUS_SIGN_TIMEOUT=60",
		"FAKE_LOG=" + f.logp,
		"FAKE_PIDFILE=" + f.pidp,
	}, env...)
	return f
}

// assertNoTemp checks no temp file is left next to the destination.
func (f *fixture) assertNoTemp(t *testing.T) {
	t.Helper()
	left, _ := filepath.Glob(f.dest + ".new.*")
	testutil.Equal(t, len(left), 0)
}

// codesignAlive reports whether the fake codesign recorded in the pidfile still runs.
func (f *fixture) codesignAlive() bool {
	b, err := os.ReadFile(f.pidp)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// waitFor polls cond until it holds or the (generous, load-tolerant) deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func run(t *testing.T, env ...string) result {
	t.Helper()
	f := newFixture(t, env...)
	start := time.Now()
	out, runErr := f.cmd.CombinedOutput()
	el := time.Since(start)
	b, _ := os.ReadFile(f.dest)
	lg, _ := os.ReadFile(f.logp)
	f.assertNoTemp(t) // temp file cleaned on every path
	waitFor(t, "codesign process to be gone after script exit", func() bool { return !f.codesignAlive() })
	return result{string(out), runErr, string(b), string(lg), el}
}

// TestSignalCleansUp: SIGTERM mid-codesign must exit, kill codesign, and remove
// the temp — not resume the script or leave a half-deploy.
func TestSignalCleansUp(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns shell + sleeps")
	}
	f := newFixture(t, "FAKE_CODESIGN=hang", "ARGUS_CODESIGN_TIMEOUT=600")
	testutil.NoError(t, f.cmd.Start())
	waitFor(t, "codesign to start", f.codesignAlive)
	testutil.NoError(t, f.cmd.Process.Signal(syscall.SIGTERM))
	done := make(chan error, 1)
	go func() { done <- f.cmd.Wait() }()
	select {
	case err := <-done:
		testutil.Equal(t, err != nil, true)
	case <-time.After(90 * time.Second):
		_ = f.cmd.Process.Kill()
		t.Fatal("script did not exit after SIGTERM")
	}
	b, _ := os.ReadFile(f.dest)
	testutil.Equal(t, string(b), "OLD\n")
	f.assertNoTemp(t)
	waitFor(t, "codesign to be killed", func() bool { return !f.codesignAlive() })
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
		r := run(t, "FAKE_CODESIGN=hang", "ARGUS_CODESIGN_TIMEOUT=2")
		testutil.Equal(t, r.err != nil, true)
		testutil.Equal(t, r.dest, "OLD\n")
		testutil.Contains(t, r.out, "timed out")
		// The fake would hang for 600s; finishing far sooner proves the kill.
		testutil.Equal(t, r.elapsed < 300*time.Second, true)
	})
	t.Run("codesign failure leaves live binary untouched", func(t *testing.T) {
		// Explicit long codesign timeout: a fast exit-1 must never race a short timer
		// into the "timed out" branch on a loaded host.
		r := run(t, "FAKE_CODESIGN=fail", "ARGUS_CODESIGN_TIMEOUT=60")
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

// TestDefaultBuildKeepsVCSStamp proves the build-to-temp path still embeds
// vcs.revision/vcs.modified (which `argus doctor` and the skew modal read), by
// running the script's real default build inside a throwaway git repo.
func TestDefaultBuildKeepsVCSStamp(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	repo := t.TempDir()
	sh := func(name string, args ...string) {
		c := exec.Command(name, args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	testutil.NoError(t, os.MkdirAll(filepath.Join(repo, "cmd", "argus"), 0o755))
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module vp\n\ngo 1.26\n"), 0o644))
	testutil.NoError(t, os.WriteFile(filepath.Join(repo, "cmd", "argus", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	sh("git", "init", "-q")
	sh("git", "-c", "user.email=t@t", "-c", "user.name=t", "add", "-A")
	sh("git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")

	dest := filepath.Join(t.TempDir(), "bin", "argus") // outside the repo: no dirty tree
	script, err := filepath.Abs("../install-signed.sh")
	testutil.NoError(t, err)
	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "ARGUS_INSTALL_BIN="+dest, "ARGUS_SKIP_SIGN=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install-signed: %v\n%s", err, out)
	}
	out, err := exec.Command("go", "version", "-m", dest).CombinedOutput()
	testutil.NoError(t, err)
	testutil.Contains(t, string(out), "vcs.revision=")
	testutil.Contains(t, string(out), "vcs.modified=false")
}
