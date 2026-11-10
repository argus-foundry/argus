package gitutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const maxUntrackedDiffBytes = 16 << 20

var errUntrackedUnavailable = errors.New("untracked file unavailable within worktree")

func untrackedFileDiff(worktree, filePath string) (string, error) {
	if !filepath.IsLocal(filePath) {
		return "", nil
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return "", nil
	}
	defer func() { _ = root.Close() }()
	private, err := os.MkdirTemp("", "argus-file-diff-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(private) }()
	destination := filepath.Join(private, filePath)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", err
	}
	remaining := int64(maxUntrackedDiffBytes)
	symlink, err := captureUntrackedFile(root, worktree, filePath, destination, &remaining)
	if err != nil {
		if errors.Is(err, errUntrackedUnavailable) {
			return "", nil
		}
		return "", err
	}
	if err := captureUntrackedAttributes(root, worktree, filePath, private, &remaining); err != nil {
		return "", err
	}
	gitDir, err := runGit(worktree, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return renderCapturedFile(worktree, private, strings.TrimSpace(gitDir), filePath, symlink)
}

func captureUntrackedAttributes(root *os.Root, worktree, filePath, private string, remaining *int64) error {
	for directory := filepath.Dir(filepath.Clean(filePath)); ; directory = filepath.Dir(directory) {
		attribute := filepath.Join(directory, ".gitattributes")
		if attribute != filepath.Clean(filePath) {
			destination := filepath.Join(private, attribute)
			symlink, err := captureUntrackedFile(root, worktree, attribute, destination, remaining)
			if err != nil && !errors.Is(err, errUntrackedUnavailable) {
				return err
			}
			if err == nil && symlink {
				if err := os.Remove(destination); err != nil {
					return err
				}
			}
		}
		if directory == "." {
			return nil
		}
	}
}
