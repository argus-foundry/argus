package gitutil

import (
	"strconv"
	"strings"
)

// parseNumstatZ parses `git diff --numstat -z`. Renames are emitted as
// "add\tdel\t\0old\0new\0"; the result is keyed by the new path.
func parseNumstatZ(out string) map[string]numstatEntry {
	m := make(map[string]numstatEntry)
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		rec := parts[i]
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\t", 3)
		if len(f) < 3 {
			continue
		}
		ns := numstatEntry{}
		if f[0] == "-" || f[1] == "-" {
			ns.binary = true
		} else {
			ns.add, _ = strconv.Atoi(f[0])
			ns.del, _ = strconv.Atoi(f[1])
		}
		path := f[2]
		if path == "" {
			if i+2 >= len(parts) {
				break
			}
			ns.orig, path = parts[i+1], parts[i+2]
			i += 2
		}
		m[path] = ns
	}
	return m
}

// parseStatusZ parses `git status --porcelain=v1 -z` into legacy-style single
// status codes ("M", "A", "D", "R", "??").
func parseStatusZ(out string) []FileChange {
	var files []FileChange
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		rec := parts[i]
		if len(rec) < 4 {
			continue
		}
		xy, path := rec[:2], rec[3:]
		fc := FileChange{Path: path, Status: collapseXY(xy)}
		if xy[0] == 'R' || xy[0] == 'C' || xy[1] == 'R' || xy[1] == 'C' {
			if i+1 < len(parts) {
				fc.OriginalPath = parts[i+1]
				i++
			}
		}
		files = append(files, fc)
	}
	return files
}

func collapseXY(xy string) string {
	if xy == "??" {
		return "??"
	}
	if strings.ContainsAny(xy, "U") || xy == "AA" || xy == "DD" {
		return "U"
	}
	if xy[1] != ' ' {
		return string(xy[1])
	}
	return string(xy[0])
}

// parseNameStatusZ parses `git diff --name-status -z`.
func parseNameStatusZ(out string) []FileChange {
	var files []FileChange
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		code := parts[i]
		if code == "" {
			continue
		}
		fc := FileChange{Status: code[:1]}
		if code[0] == 'R' || code[0] == 'C' {
			if i+2 >= len(parts) {
				break
			}
			fc.OriginalPath, fc.Path = parts[i+1], parts[i+2]
			i += 2
		} else {
			if i+1 >= len(parts) {
				break
			}
			fc.Path = parts[i+1]
			i++
		}
		files = append(files, fc)
	}
	return files
}
