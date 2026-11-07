package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestUntrackedDiffTextconvRawInput(t *testing.T) {
	for _, kind := range []string{"clean", "required-clean", "required-clean-smudge", "smudge", "eol", "quoted", "swap"} {
		t.Run(kind, func(t *testing.T) {
			dir := initRepo(t, t.TempDir())
			attrs := "*.txt filter=custom diff=custom\n"
			content := "lower\n"
			switch kind {
			case "eol":
				attrs = "*.txt text eol=lf diff=custom\n"
				content = "lower\r\n"
			case "smudge":
				gitRun(t, dir, "config", "filter.custom.smudge", "tr a-z A-Z")
			default:
				gitRun(t, dir, "config", "filter.custom.clean", "tr a-z A-Z")
				if kind != "clean" {
					gitRun(t, dir, "config", "filter.custom.required", "true")
				}
				if kind == "required-clean-smudge" {
					gitRun(t, dir, "config", "filter.custom.smudge", "cat")
				}
			}
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(attrs), 0o600))
			script := "#!/bin/sh\ncat \"$1\"\n"
			command := "./render.sh"
			name := "data.txt"
			if kind == "quoted" {
				script = "#!/bin/sh\n[ \"$1\" = 'argument with spaces' ] || exit 42\ncat \"$2\"\n"
				command = "./render.sh 'argument with spaces'"
				name = "data ' quote.txt"
				temporary := filepath.Join(t.TempDir(), "capture ' copies")
				testutil.NoError(t, os.Mkdir(temporary, 0o700))
				t.Setenv("TMPDIR", temporary)
			}
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, "render.sh"), []byte(script), 0o700))
			gitRun(t, dir, "config", "diff.custom.textconv", command)
			testutil.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
			expected, _ := runGit(dir, "diff", "--no-index", "--", "/dev/null", name)
			testutil.Contains(t, expected, "+"+content)
			outside := ""
			if kind == "swap" {
				realGit, err := exec.LookPath("git")
				testutil.NoError(t, err)
				outside = filepath.Join(t.TempDir(), "outside")
				testutil.NoError(t, os.WriteFile(outside, []byte("OUTSIDE\n"), 0o600))
				bin := t.TempDir()
				const shim = `#!/bin/sh
previous=
for arg do
  if [ "$previous" = "--cached" ] && [ "$arg" != "--" ]; then
    mv "$PR1139_ORIGINAL" "$PR1139_SAVED" || exit 70
    ln -s "$PR1139_OUTSIDE" "$PR1139_ORIGINAL" || exit 71
    break
  fi
  previous=$arg
done
exec "$PR1139_GIT" "$@"
`
				testutil.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700))
				t.Setenv("PR1139_GIT", realGit)
				t.Setenv("PR1139_ORIGINAL", filepath.Join(dir, name))
				t.Setenv("PR1139_SAVED", filepath.Join(dir, "saved"))
				t.Setenv("PR1139_OUTSIDE", outside)
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			got := FetchFileDiff("raw", dir, name, Options{})
			testutil.NoError(t, got.Err)
			testutil.Equal(t, got.Diff, expected)
			if kind == "swap" {
				target, err := os.Readlink(filepath.Join(dir, name))
				testutil.NoError(t, err)
				testutil.Equal(t, target, outside)
			}
		})
	}
}
