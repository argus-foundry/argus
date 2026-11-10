package claudeartifacts

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestValidateHTML(t *testing.T) {
	oversized := append([]byte("<!DOCTYPE html>"), bytes.Repeat([]byte("a"), MaxContentBytes)...)
	for _, tc := range []struct {
		name, file string
		content    []byte
		wantErr    string
	}{
		{name: "doctype", file: "p.html", content: []byte(page)},
		{name: "lowercase doctype htm", file: "p.htm", content: []byte("<!doctype html><p>x")},
		{name: "html tag uppercase ext", file: "P.HTML", content: []byte("<html><body/></html>")},
		{name: "leading whitespace and BOM", file: "p.html", content: []byte("\uFEFF\n  <!DOCTYPE HTML>")},
		{name: "exactly at cap", file: "p.html", content: append([]byte("<html>"), bytes.Repeat([]byte("a"), MaxContentBytes-6)...)},
		{name: "markdown extension", file: "notes.md", content: []byte(page), wantErr: ".html or .htm"},
		{name: "no extension", file: "page", content: []byte(page), wantErr: ".html or .htm"},
		{name: "missing doctype", file: "p.html", content: []byte("<div>not a page</div>"), wantErr: "<!DOCTYPE html>"},
		{name: "empty", file: "p.html", content: nil, wantErr: "<!DOCTYPE html>"},
		{name: "too large", file: "p.html", content: oversized, wantErr: "16 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateHTML(tc.file, tc.content)
			if tc.wantErr == "" {
				testutil.NoError(t, err)
				return
			}
			testutil.ErrorIs(t, err, ErrInvalidInput)
			testutil.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestClientPublish_InvalidInputMakesNoRequest pins that every local
// rejection happens before the endpoint is contacted.
func TestClientPublish_InvalidInputMakesNoRequest(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		content    []byte
	}{
		{"non-html extension", "report.pdf", []byte(page)},
		{"missing doctype", "p.html", []byte("hello")},
		{"oversized", "p.html", append([]byte("<html>"), bytes.Repeat([]byte("a"), MaxContentBytes)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, srv := newFakeEndpoint(t, publishHandler(t))
			_, err := testClient(srv).Publish(context.Background(), fakeBearer, PublishInput{Name: tc.file, Content: tc.content, Title: "T"})
			testutil.ErrorIs(t, err, ErrInvalidInput)
			testutil.Equal(t, f.count(), 0)
		})
	}
}

func TestReadHTMLFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		testutil.NoError(t, os.WriteFile(p, data, 0o600))
		return p
	}
	good := write("good.html", []byte(page))
	big := write("big.html", append([]byte("<html>"), bytes.Repeat([]byte("a"), MaxContentBytes)...))
	md := write("notes.md", []byte(page))
	testutil.NoError(t, os.Mkdir(filepath.Join(dir, "dir.html"), 0o700))

	t.Run("reads a valid page", func(t *testing.T) {
		got, err := ReadHTMLFile(good)
		testutil.NoError(t, err)
		testutil.Equal(t, string(got), page)
	})
	for _, tc := range []struct{ name, path, want string }{
		{"oversized rejected from stat", big, "16 MiB"},
		{"wrong extension", md, ".html or .htm"},
		{"directory", filepath.Join(dir, "dir.html"), "not a regular file"},
		{"missing", filepath.Join(dir, "nope.html"), "nope.html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadHTMLFile(tc.path)
			testutil.Error(t, err)
			testutil.Contains(t, err.Error(), tc.want)
		})
	}
}
