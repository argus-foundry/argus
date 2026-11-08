//go:build !darwin

package agent

// The orphan-browser sweep is macOS-only (reap-orphaned-playwright-browsers
// Non-Goals): elsewhere the snapshot is empty, so nothing is ever closed.

func browserSnapshot() ([]BrowserProc, error) { return nil, nil }

func browserProcByPID(int32) (BrowserProc, bool, error) { return BrowserProc{}, false, nil }

func browserProcArgs(int32) ([]string, []string, error) { return nil, nil, nil }
