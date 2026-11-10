package agent

// TestingT is the subset of testing.TB the sandbox seam needs; declared here so
// production code does not import "testing".
type TestingT interface {
	Helper()
	Cleanup(func())
}

// ForceSandboxAvailableForTest forces IsSandboxAvailable to report available for
// the rest of the test, restoring the real probe via t.Cleanup. TEST-ONLY: it
// lets cross-package tests exercise sandbox-gated guards on hosts (CI/Linux)
// without sandbox-exec. Production code never calls it.
func ForceSandboxAvailableForTest(t TestingT, available bool) {
	t.Helper()
	ResetSandboxCache()
	sandboxOnce.Do(func() { sandboxExists = available })
	t.Cleanup(ResetSandboxCache)
}
