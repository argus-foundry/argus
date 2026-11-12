package gitutil

import "strings"

type fileCountAttributes struct {
	diff       string
	textconv   bool
	conversion bool
}

func (a fileCountAttributes) convertsUntracked() bool {
	return a.conversion || a.diff != "" && a.diff != "unspecified" && a.diff != "set" && a.diff != "unset"
}

func collectCountAttributes(worktree string, files []FileChange) (map[string]fileCountAttributes, error) {
	attrs := make(map[string]fileCountAttributes)
	var paths strings.Builder
	for _, f := range files {
		if _, ok := attrs[f.Path]; ok || strings.HasSuffix(f.Path, "/") {
			continue
		}
		attrs[f.Path] = fileCountAttributes{}
		paths.WriteString(f.Path)
		paths.WriteByte(0)
	}
	if len(attrs) == 0 {
		return attrs, nil
	}
	out, err := runGitWithInput(worktree, strings.NewReader(paths.String()), nil, "check-attr", "-z", "--stdin", "diff", "text", "filter", "working-tree-encoding", "eol", "ident")
	if err != nil {
		return nil, err
	}
	config, err := untrackedConfig(worktree, `^diff\..*\.textconv$`, false)
	if err != nil {
		return nil, err
	}
	drivers := make(map[string]bool)
	for _, record := range strings.Split(config, "\x00") {
		key, _, ok := strings.Cut(record, "\n")
		if ok {
			drivers[strings.TrimSuffix(strings.TrimPrefix(key, "diff."), ".textconv")] = true
		}
	}
	records := strings.Split(out, "\x00")
	for i := 0; i+2 < len(records); i += 3 {
		path, name, value := records[i], records[i+1], records[i+2]
		a := attrs[path]
		if name == "diff" {
			a.diff = value
			a.textconv = drivers[value]
		} else if value != "unspecified" && value != "unset" {
			a.conversion = true
		}
		attrs[path] = a
	}
	return attrs, nil
}
