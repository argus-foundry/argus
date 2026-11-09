// Package testenv makes the Go test suite hermetic with respect to the
// environment of whatever agent is running it.
//
// An argus/hera worker (Claude or Codex) exports its own identity into every
// child process: ARGUS_TASK_ID, ARGUS_MODEL, CODEX_HOME, CLAUDE_CODE_SESSION_ID
// and friends. `go test` inherits all of it, and production code that reads
// the ambient environment (BuildCmd's profile env, Codex session-ID capture,
// the Claude session-marker scrub) then sees the AGENT's state instead of the
// test's fixture. CI exports none of these, so the suite passes there and
// false-fails in a worker sandbox.
//
// Every test package installs this scrub from a TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }
//
// A test that needs one of these variables sets it itself with t.Setenv; the
// scrub only guarantees a clean baseline. The AST guard in this package fails
// if a package with tests forgets the TestMain.
package testenv

import (
	"os"
	"slices"
	"testing"

	"github.com/argus-foundry/argus/internal/claudeenv"
)

// argusOwned are the variables Argus itself exports to (or reads from) an
// agent session. This is an explicit list, not an ARGUS_* prefix strip: tests
// own ARGUS_TEST_* helper-process variables that must survive.
var argusOwned = []string{
	"ARGUS_TASK_ID",
	"ARGUS_ARCHETYPE",
	"ARGUS_MODEL",
	"ARGUS_PROFILE",
	"ARGUS_SANDBOXED",
	"ARGUS_BOUNCED",
	"ARGUS_BRIDGE_GENERATION",
	"ARGUS_HOST_SUSPENDED",
	"ARGUS_TOKEN",
	"ARGUS_DATA_DIR",
	"ARGUS_TYPING_LOG",
}

// codexOwned are the Codex CLI's per-session/home variables. CODEX_HOME and
// CODEX_SQLITE_HOME redirect Codex session-ID capture to the live user DB.
var codexOwned = []string{
	"CODEX_HOME",
	"CODEX_SQLITE_HOME",
	"CODEX_THREAD_ID",
	"CODEX_API_KEY",
}

// otherAgent are backend config/credential variables that BuildCmd and the
// account/spawn-env code read or strip.
var otherAgent = []string{
	"CLAUDE_CONFIG_DIR",
	"OPENCODE_CONFIG_CONTENT",
	"OPENAI_API_KEY",
}

// Names returns the full denylist: every variable Main removes.
func Names() []string {
	var out []string
	out = append(out, argusOwned...)
	out = append(out, codexOwned...)
	out = append(out, otherAgent...)
	out = append(out, claudeenv.ParentSessionMarkers...)
	out = append(out, claudeenv.AuthOverrides...)
	out = append(out, claudeenv.ForcePersistenceVar)
	slices.Sort(out)
	return slices.Compact(out)
}

// Scrub unsets every denylisted variable and returns the names that were set.
func Scrub() []string {
	var removed []string
	for _, name := range Names() {
		if _, ok := os.LookupEnv(name); !ok {
			continue
		}
		removed = append(removed, name)
		if err := os.Unsetenv(name); err != nil {
			panic("testenv: unsetenv " + name + ": " + err.Error())
		}
	}
	return removed
}

// Main scrubs the environment and runs the package's tests. Use as
// os.Exit(testenv.Main(m)); packages with their own setup call Scrub directly
// before it.
func Main(m *testing.M) int {
	Scrub()
	return m.Run()
}

// AssertScrubbed fails the test if any denylisted variable is set. Call it
// from a package-level guard test: it proves the package's TestMain ran the
// scrub even when the surrounding shell is a contaminated worker env.
func AssertScrubbed(t testing.TB) {
	t.Helper()
	for _, name := range Names() {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("%s leaked into the test process (len %d): TestMain must call testenv.Main/Scrub", name, len(v))
		}
	}
}
