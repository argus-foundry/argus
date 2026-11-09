package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/argus-foundry/argus/internal/codexctx"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

const maxCodexMoveMetadataBytes = 4 << 20

var errCodexRolloutMissing = errors.New("source rollout missing")

func sameCodexMoveHome(srcHome, dstHome, srcDB, dstDB string) bool {
	if canonPath(srcHome) == canonPath(dstHome) {
		return true
	}
	src, srcErr := os.Stat(srcDB)
	dst, dstErr := os.Stat(dstDB)
	return srcErr == nil && dstErr == nil && os.SameFile(src, dst)
}

func codexRolloutRel(srcHome, rollout string) (string, error) {
	rel, err := filepath.Rel(filepath.Join(srcHome, "sessions"), rollout)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return "", fmt.Errorf("%w: rollout is outside the source sessions tree", ErrAccount)
	}
	return rel, nil
}

// transferCodexRollout anchors reads and writes to their account roots. The
// previous destination stays recoverable until its index write succeeds.
func transferCodexRollout(srcHome, dstHome, cwd string, thread codexctx.Thread, index func(string) error) error {
	rollout := thread.RolloutPath
	if rollout == "" {
		return errCodexRolloutMissing
	}
	rel, err := codexRolloutRel(srcHome, rollout)
	if err != nil {
		rel, err = codexRolloutRel(canonPath(srcHome), canonPath(rollout))
	}
	if err != nil {
		return err
	}
	src, err := os.OpenRoot(srcHome)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	input, err := openCodexRolloutSource(src, rel)
	if err != nil {
		if errors.Is(err, ErrAccount) {
			return err
		}
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %w", errCodexRolloutMissing, err)
		}
		return fmt.Errorf("%w: unsafe source rollout: %w", ErrAccount, err)
	}
	defer func() { _ = input.Close() }()
	if _, err := codexctx.ValidateThreadRolloutWithLimit(input, cwd, thread, maxCodexMoveMetadataBytes); err != nil {
		return fmt.Errorf("%w: invalid Codex rollout metadata: %w", ErrAccount, err)
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("%w: cannot rewind Codex rollout: %w", ErrAccount, err)
	}
	indexHome := dstHome
	dstHome, err = filepath.EvalSymlinks(dstHome)
	if err != nil {
		return err
	}
	dst, err := os.OpenRoot(dstHome)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()
	name := filepath.Join("sessions", rel)
	if err := prepareCodexRolloutDestination(dst, name); err != nil {
		return err
	}
	destinationPath := filepath.Join(indexHome, name)
	parent, err := openCodexRolloutDirectories(dst, filepath.Dir(name))
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	name = filepath.Base(name)
	if err := prepareCodexRolloutDestination(parent, name); err != nil {
		return err
	}
	staged := ".argus-move-" + model.GenerateSessionID()
	output, err := parent.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Remove(staged) }()
	_, copyErr := io.Copy(output, input)
	if err := errors.Join(copyErr, output.Close()); err != nil {
		return err
	}
	backup := staged + ".previous"
	existed := false
	if _, err := parent.Lstat(name); err == nil {
		if err := parent.Link(name, backup); err != nil {
			return err
		}
		existed = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := parent.Rename(staged, name); err != nil {
		if existed {
			_ = parent.Remove(backup)
		}
		return err
	}
	if err := index(destinationPath); err != nil {
		var restoreErr error
		if existed {
			restoreErr = parent.Rename(backup, name)
		} else {
			restoreErr = parent.Remove(name)
		}
		uxlog.Log("[account] move: codex index failed; rollout restored=%t err=%v restore_err=%v", restoreErr == nil, err, restoreErr)
		return fmt.Errorf("index thread: %w", errors.Join(err, restoreErr))
	}
	if existed {
		if err := parent.Remove(backup); err != nil {
			uxlog.Log("[account] move: cannot remove prior rollout backup %s: %v", backup, err)
		}
	}
	return nil
}

func prepareCodexRolloutDestination(root *os.Root, name string) error {
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	for i := range parts {
		path := filepath.Join(parts[:i+1]...)
		info, err := root.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			if i == len(parts)-1 {
				return nil
			}
			if err := root.Mkdir(path, 0700); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: destination rollout path contains a symlink", ErrAccount)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("%w: destination rollout parent is not a directory", ErrAccount)
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return fmt.Errorf("%w: destination rollout is not a regular file", ErrAccount)
		}
	}
	return nil
}
