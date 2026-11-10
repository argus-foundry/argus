package agent

// TestingT is the subset of testing.TB the sandbox seam needs; declared here so
// production code does not import "testing".
type TestingT interface {
	Helper()
	Cleanup(func())
}

// sandboxSeamInTest gates ForceSandboxAvailableForTest; a var only so the
// refusal itself can be tested.
var sandboxSeamInTest = isTestBinary

// ForceSandboxAvailableForTest forces IsSandboxAvailable to report available for
// the rest of the test, restoring the real probe via t.Cleanup. TEST-ONLY: it
// lets cross-package tests exercise sandbox-gated guards on hosts (CI/Linux)
// without sandbox-exec. Outside a test binary it panics instead of rewriting
// the availability cache, which would otherwise bypass the unattended-worker
// sandbox gate.
func ForceSandboxAvailableForTest(t TestingT, available bool) {
	t.Helper()
	if !sandboxSeamInTest() {
		panic("agent.ForceSandboxAvailableForTest called outside a test binary; refusing to override sandbox availability")
	}
	ResetSandboxCache()
	sandboxOnce.Do(func() { sandboxExists = available })
	t.Cleanup(ResetSandboxCache)
}

// HoldAccountMoveForTest marks taskID as having an account move in flight for
// the rest of the test (released via t.Cleanup), so cross-package tests can
// pin guards keyed on AccountMoveInFlight. TEST-ONLY: panics outside a test
// binary, like ForceSandboxAvailableForTest.
func HoldAccountMoveForTest(t TestingT, taskID string) {
	t.Helper()
	if !sandboxSeamInTest() {
		panic("agent.HoldAccountMoveForTest called outside a test binary")
	}
	if !claimAccountMove(taskID) {
		panic("agent.HoldAccountMoveForTest: move already in flight for " + taskID)
	}
	t.Cleanup(func() { releaseAccountMove(taskID) })
}
