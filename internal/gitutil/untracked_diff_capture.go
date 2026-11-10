package gitutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func captureUntrackedFile(root *os.Root, worktree, filePath, destination string, remaining *int64) (bool, error) {
	parent, err := untrackedRelativePath(worktree, filepath.Join(worktree, filepath.Dir(filePath)))
	if err != nil {
		return false, errUntrackedUnavailable
	}
	entry := filepath.Join(parent, filepath.Base(filePath))
	info, err := root.Lstat(entry)
	if err != nil {
		return false, errUntrackedUnavailable
	}
	return captureUntrackedEntry(root, worktree, filePath, entry, destination, info.Mode(), remaining)
}

func captureUntrackedEntry(root *os.Root, worktree, filePath, entry, destination string, mode os.FileMode, remaining *int64) (bool, error) {
	if mode&os.ModeSymlink != 0 {
		return true, captureUntrackedSymlink(root, worktree, entry, destination, remaining)
	}
	resolved, err := untrackedRelativePath(worktree, filepath.Join(worktree, filePath))
	if err != nil {
		return false, errUntrackedUnavailable
	}
	return false, copyUntrackedFile(root, resolved, destination, remaining)
}

func captureUntrackedSymlink(root *os.Root, worktree, entry, destination string, remaining *int64) error {
	target, err := root.Readlink(entry)
	if err != nil {
		return errUntrackedUnavailable
	}
	fullTarget := target
	if !filepath.IsAbs(target) {
		fullTarget = filepath.Join(worktree, filepath.Dir(entry), target)
	}
	resolved, err := untrackedRelativePath(worktree, fullTarget)
	if err != nil {
		return errUntrackedUnavailable
	}
	file, err := root.OpenFile(resolved, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return errUntrackedUnavailable
	}
	file.Close()
	if int64(len(target)) > *remaining {
		return fmt.Errorf("untracked snapshot exceeds %d bytes", maxUntrackedDiffBytes)
	}
	if err := os.WriteFile(destination, []byte(target), 0o600); err != nil {
		return err
	}
	*remaining -= int64(len(target))
	return nil
}

func untrackedRelativePath(worktree, name string) (string, error) {
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(relative) {
		return "", errUntrackedUnavailable
	}
	return relative, nil
}

func copyUntrackedFile(root *os.Root, relative, destination string, remaining *int64) error {
	file, err := root.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return errUntrackedUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errUntrackedUnavailable
	}
	if info.Size() > *remaining {
		return fmt.Errorf("untracked snapshot exceeds %d bytes", maxUntrackedDiffBytes)
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	size, err := io.Copy(out, io.LimitReader(file, *remaining+1))
	if err != nil {
		return err
	}
	if size > *remaining {
		return fmt.Errorf("untracked snapshot exceeds %d bytes", maxUntrackedDiffBytes)
	}
	*remaining -= size
	return out.Chmod(info.Mode().Perm())
}
