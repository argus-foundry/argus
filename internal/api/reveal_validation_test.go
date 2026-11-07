package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestRevealRejectsTrailingBody(t *testing.T) {
	for _, tc := range []struct{ name, suffix string }{
		{"garbage", " junk"}, {"second object", " {}"}, {"oversized suffix", strings.Repeat(" ", 9000) + "junk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h, id, _, calls := revealFixture(t)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", `{}`+tc.suffix))
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			testutil.Equal(t, len(*calls), 0)
		})
	}
}

func TestRevealRequiresWorktreeDirectory(t *testing.T) {
	for _, kind := range []string{"regular file", "directory symlink", "app bundle symlink", "dangling symlink", "real directory"} {
		t.Run(kind, func(t *testing.T) {
			_, h, id, root, calls := revealFixture(t)
			if kind != "real directory" {
				testutil.NoError(t, os.Remove(root))
				switch kind {
				case "regular file":
					testutil.NoError(t, os.WriteFile(root, []byte("file"), 0o600))
				default:
					target := filepath.Join(t.TempDir(), "target")
					if kind == "app bundle symlink" {
						target += ".app"
					}
					if kind != "dangling symlink" {
						testutil.NoError(t, os.Mkdir(target, 0o700))
					}
					testutil.NoError(t, os.Symlink(target, root))
				}
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, masterReq("POST", "/api/tasks/"+id+"/reveal", `{}`))
			if kind == "real directory" {
				testutil.Equal(t, w.Code, http.StatusOK)
				testutil.Equal(t, len(*calls), 1)
				testutil.Equal(t, (*calls)[0].name, "open")
				testutil.DeepEqual(t, (*calls)[0].args, []string{"-a", "Finder", root})
			} else {
				testutil.Equal(t, w.Code, http.StatusBadRequest)
				testutil.Equal(t, decodeBody(t, w)["error"], any("invalid_path"))
				testutil.Equal(t, len(*calls), 0)
			}
		})
	}
}
