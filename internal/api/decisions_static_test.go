package api

import (
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestDecisionStaticAssets(t *testing.T) {
	page, err := staticFS.ReadFile("static/decide.html")
	testutil.NoError(t, err)
	text := string(page)
	for _, forbidden := range []string{"https://", "http://", "innerHTML", "fonts.googleapis", "artifact-database"} {
		t.Run(forbidden, func(t *testing.T) { testutil.Equal(t, strings.Contains(text, forbidden), false) })
	}
	testutil.Equal(t, strings.Index(text, "history.replaceState") < strings.Index(text, "await fetch("), true)
	sw, err := staticFS.ReadFile("static/sw.js")
	testutil.NoError(t, err)
	source := string(sw)
	testutil.Contains(t, source, "url.pathname === '/decide'")
	testutil.Contains(t, source, "argus-shell-v96")
	shell := source[strings.Index(source, "const SHELL_ASSETS"):strings.Index(source, "];")]
	testutil.Equal(t, strings.Contains(shell, "/decide"), false)
}
