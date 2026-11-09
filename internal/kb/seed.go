package kb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SeedVaultFile writes content to the vault-relative path and indexes it, but
// only when no file exists there yet; it never overwrites. It returns whether
// the file was created. The path must be vault-relative with no escaping.
func SeedVaultFile(store KBStore, vault, rel, content string) (bool, error) {
	clean := filepath.Clean(rel)
	if rel == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("seed %q: path must be vault-relative with no '..' components", rel)
	}
	if vault == "" {
		return false, errors.New("seed: vault path is empty")
	}
	abs := filepath.Join(vault, clean)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return false, err
	}
	doc := ParseDocument(filepath.ToSlash(clean), content)
	// O_EXCL makes "absent" and "create" one atomic step.
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // vault notes are user-readable markdown, not secrets
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, err
	}
	_, werr := f.WriteString(RenderMarkdown(&doc))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(abs) //nolint:errcheck
		return false, werr
	}
	now := time.Now()
	doc.IngestedAt, doc.ModifiedAt = now, now
	if err := store.KBUpsert(&doc); err != nil {
		return true, err
	}
	return true, nil
}
