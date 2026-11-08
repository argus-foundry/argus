//go:build !darwin

package agent

// The browser census is macOS-only: elsewhere the snapshot is empty.

func browserSnapshot() ([]BrowserProc, error) { return nil, nil }

func browserProcArgs(int32) ([]string, []string, error) { return nil, nil, nil }
