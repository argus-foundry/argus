//go:build !darwin

package mcp

import (
	"os"
	"path/filepath"
)

// descriptorPathCanonical is false off darwin: there is no F_GETPATH, so the
// path comes from resolving the name, which is weaker (no case
// canonicalization; a parent swapped after the open is caught only by the
// same-file check in confinedOpen).
const descriptorPathCanonical = false

// descriptorPath resolves f's name. See descriptorPathCanonical.
func descriptorPath(f *os.File) (string, error) {
	return filepath.EvalSymlinks(f.Name())
}
