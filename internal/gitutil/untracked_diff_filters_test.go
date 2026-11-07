package gitutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestUntrackedDiffInputConversion(t *testing.T) {
	for _, kind := range []string{"eol", "eol-safecrlf", "clean"} {
		t.Run(kind, func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			attributes := "*.txt text eol=lf\n"
			content := "LINE\r\n"
			if kind == "clean" {
				attributes = "*.txt filter=custom\n"
				content = "unclean\n"
				testutil.NoError(t, os.WriteFile(filepath.Join(dir, "clean.sh"), []byte("#!/bin/sh\ntr '[:lower:]' '[:upper:]'\n"), 0o700))
				gitRun(t, dir, "config", "filter.custom.clean", "./clean.sh")
				gitRun(t, dir, "config", "filter.custom.required", "true")
			} else {
				gitRun(t, dir, "config", "core.autocrlf", "true")
				if kind == "eol-safecrlf" {
					gitRun(t, dir, "config", "core.safecrlf", "true")
				}
			}
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(attributes), 0o600))
			testutil.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0o700))
			for _, name := range []string{"crlf.txt", "nested/space name.txt", "--dash.txt", "*.txt"} {
				t.Run(name, func(t *testing.T) {
					testutil.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o700))
					expected, _ := runGit(dir, "diff", "--no-index", "--", "/dev/null", name)
					testutil.NotEqual(t, expected, "")
					got := FetchFileDiff("conversion", dir, name, Options{})
					testutil.NoError(t, got.Err)
					testutil.Equal(t, got.Diff, expected)
				})
			}
		})
	}
}
