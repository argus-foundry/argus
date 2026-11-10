package claudeartifacts

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxContentBytes caps a published page: the endpoint's own content limit.
const MaxContentBytes = 16 << 20

// ValidateHTML checks a page before any network call: name must end in .html
// or .htm, content must fit in MaxContentBytes, and it must start (after
// whitespace and a byte-order mark) with an HTML doctype or an <html> tag.
func ValidateHTML(name string, content []byte) error {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".htm":
	default:
		return fmt.Errorf("%w: %q must be an .html or .htm file", ErrInvalidInput, filepath.Base(name))
	}
	if len(content) > MaxContentBytes {
		return fmt.Errorf("%w: %q is %d bytes; the limit is 16 MiB", ErrInvalidInput, filepath.Base(name), len(content))
	}
	head := bytes.TrimLeft(bytes.TrimPrefix(bytes.TrimLeft(content, " \t\r\n"), []byte("\uFEFF")), " \t\r\n")
	if len(head) > 64 {
		head = head[:64]
	}
	lower := bytes.ToLower(head)
	if !bytes.HasPrefix(lower, []byte("<!doctype html")) && !bytes.HasPrefix(lower, []byte("<html")) {
		return fmt.Errorf("%w: %q must start with <!DOCTYPE html> or <html>", ErrInvalidInput, filepath.Base(name))
	}
	return nil
}

// ReadHTMLFile reads and validates a local page. The size cap is checked from
// the file's metadata before it is read, and again on the bytes read.
func ReadHTMLFile(path string) ([]byte, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
	default:
		return nil, fmt.Errorf("%w: %q must be an .html or .htm file", ErrInvalidInput, filepath.Base(path))
	}
	f, err := os.Open(path) //nolint:gosec // G304: the caller names the page to publish; it is read, never executed.
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only handle
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrInvalidInput, path)
	}
	if info.Size() > MaxContentBytes {
		return nil, fmt.Errorf("%w: %q is %d bytes; the limit is 16 MiB", ErrInvalidInput, filepath.Base(path), info.Size())
	}
	content, err := io.ReadAll(io.LimitReader(f, MaxContentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := ValidateHTML(path, content); err != nil {
		return nil, err
	}
	return content, nil
}

// slugRe matches an artifact slug: the last path segment of its link.
var slugRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ParseArtifactSlug extracts the slug from a claude.ai/artifact/<slug> or
// claude.ai/code/artifact/<slug> link. A bare slug is accepted as-is.
func ParseArtifactSlug(link string) (string, error) {
	link = strings.TrimSpace(link)
	if slugRe.MatchString(link) {
		return link, nil
	}
	u, err := url.Parse(link)
	if err != nil || (u.Host != "claude.ai" && u.Host != "www.claude.ai") {
		return "", fmt.Errorf("%w: %q is not a claude.ai artifact link", ErrInvalidInput, link)
	}
	path := strings.Trim(u.Path, "/")
	var slug string
	for _, prefix := range []string{"artifact/", "code/artifact/"} {
		if rest, ok := strings.CutPrefix(path, prefix); ok {
			slug = rest
			break
		}
	}
	if !slugRe.MatchString(slug) {
		return "", fmt.Errorf("%w: %q is not a claude.ai artifact link", ErrInvalidInput, link)
	}
	return slug, nil
}
