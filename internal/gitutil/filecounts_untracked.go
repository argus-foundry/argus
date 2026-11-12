package gitutil

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"

	"github.com/argus-foundry/argus/internal/uxlog"
)

func countUntracked(worktree string, e FileChange) FileChange {
	return countUntrackedAttributes(worktree, e, fileCountAttributes{})
}

func countUntrackedAttributes(worktree string, e FileChange, attrs fileCountAttributes) FileChange {
	e.Kind, e.Additions, e.Deletions = FileKindUnavailable, nil, nil
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return e
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(e.Path)
	if err != nil {
		return e
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if !info.Mode().IsRegular() || info.Size() > maxUntrackedCountBytes {
			return e
		}
		if attrs.diff == "unset" {
			e.Kind = FileKindBinary
			return e
		}
		if attrs.textconv || attrs.convertsUntracked() {
			return e
		}
	}
	return countUntrackedEntry(root, e, info, attrs.diff == "set")
}

func countUntrackedEntry(root *os.Root, e FileChange, info os.FileInfo, forceText bool) FileChange {
	e.Kind, e.Additions, e.Deletions = FileKindUnavailable, nil, nil
	if info.Mode()&os.ModeSymlink != 0 {
		if _, err := root.Readlink(e.Path); err != nil {
			return e
		}
		one, zero := 1, 0
		e.Kind, e.Additions, e.Deletions = FileKindText, &one, &zero
		return e
	}
	if !info.Mode().IsRegular() || info.Size() > maxUntrackedCountBytes {
		return e
	}
	file, err := root.OpenFile(e.Path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return e
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) || opened.Size() > maxUntrackedCountBytes {
		return e
	}
	return countUntrackedContent(file, e, forceText)
}

var untrackedCountBuffers = sync.Pool{New: func() any { return new([32 << 10]byte) }}

func countUntrackedContent(reader io.Reader, e FileChange, forceText bool) FileChange {
	e.Kind, e.Additions, e.Deletions = FileKindUnavailable, nil, nil
	limited := io.LimitReader(reader, maxUntrackedCountBytes+1)
	buffer := untrackedCountBuffers.Get().(*[32 << 10]byte)
	defer untrackedCountBuffers.Put(buffer)
	prefix := buffer[:binarySniffBytes]
	n, err := io.ReadFull(limited, prefix)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return e
	}
	if !forceText && bytes.IndexByte(prefix[:n], 0) >= 0 {
		e.Kind = FileKindBinary
		return e
	}
	total, lines, last := n, bytes.Count(prefix[:n], []byte{'\n'}), byte('\n')
	if n > 0 {
		last = prefix[n-1]
	}
	for err == nil {
		n, err = limited.Read(buffer[:])
		total += n
		if total > maxUntrackedCountBytes {
			uxlog.Log("[git-counts] skipped growing untracked file path=%q", e.Path)
			return e
		}
		lines += bytes.Count(buffer[:n], []byte{'\n'})
		if n > 0 {
			last = buffer[n-1]
		}
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return e
	}
	if total > 0 && last != '\n' {
		lines++
	}
	zero := 0
	e.Kind, e.Additions, e.Deletions = FileKindText, &lines, &zero
	return e
}
