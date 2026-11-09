package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func openCodexRolloutDirectories(root *os.Root, path string) (*os.Root, error) {
	if !filepath.IsLocal(path) {
		return nil, fmt.Errorf("%w: unsafe rollout directory", ErrAccount)
	}
	current := root
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		next, err := openCodexRolloutDirectory(current, part)
		if current != root {
			_ = current.Close()
		}
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

func openCodexRolloutDirectory(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: rollout directory is a symlink or not a directory", ErrAccount)
	}
	opened, err := parent.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("%w: rollout directory changed during open: %w", ErrAccount, err)
	}
	after, err := opened.Stat(".")
	if err != nil {
		_ = opened.Close()
		return nil, fmt.Errorf("%w: cannot verify opened rollout identity: %w", ErrAccount, err)
	}
	if !os.SameFile(before, after) {
		_ = opened.Close()
		return nil, fmt.Errorf("%w: rollout directory changed during open", ErrAccount)
	}
	return opened, nil
}

func openCodexRolloutSource(root *os.Root, rel string) (*os.File, error) {
	parent, err := openCodexRolloutDirectories(root, filepath.Join("sessions", filepath.Dir(rel)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	name := filepath.Base(rel)
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: source rollout is a symlink or not a regular file", ErrAccount)
	}
	opened, err := parent.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: source rollout changed during open: %w", ErrAccount, err)
	}
	after, err := opened.Stat()
	if err != nil {
		_ = opened.Close()
		return nil, fmt.Errorf("%w: cannot verify opened rollout identity: %w", ErrAccount, err)
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		_ = opened.Close()
		return nil, fmt.Errorf("%w: source rollout changed during open", ErrAccount)
	}
	stat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		_ = opened.Close()
		return nil, fmt.Errorf("%w: source rollout must have exactly one link", ErrAccount)
	}

	return opened, nil
}
