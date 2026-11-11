package inject

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/text/unicode/norm"

	"github.com/argus-foundry/argus/internal/inject/atomicfile"
)

var claudeTrustMu sync.Mutex

// TrustClaudeWorktree updates only this worktree's persisted folder trust.
// The caller supplies the launched account's global config file. Read afresh
// each time: Claude also writes this file. The mutex serializes Argus callers;
// atomic rename protects readers, but cannot lock out Claude's own writers.
func TrustClaudeWorktree(path, worktree string) error {
	canonical, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return fmt.Errorf("trust claude: resolve worktree: %w", err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return err
	}
	canonical = norm.NFC.String(canonical)
	claudeTrustMu.Lock()
	defer claudeTrustMu.Unlock()
	data, err := atomicfile.ReadRawJSONObject(path, "trust claude")
	if err != nil {
		return err
	}
	projects, err := trustObject(data["projects"])
	if err != nil {
		return fmt.Errorf("trust claude: projects: %w", err)
	}
	project, err := trustObject(projects[canonical])
	if err != nil {
		return fmt.Errorf("trust claude: project: %w", err)
	}
	if string(project["hasTrustDialogAccepted"]) == "true" {
		return nil
	}
	project["hasTrustDialogAccepted"] = json.RawMessage("true")
	projects[canonical], err = json.Marshal(project)
	if err != nil {
		return err
	}
	data["projects"], err = json.Marshal(projects)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.WriteJSON(path, data, "trust claude")
}

func trustObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if len(raw) == 0 {
		return obj, nil
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return obj, nil
}
