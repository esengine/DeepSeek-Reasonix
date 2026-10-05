package main

import (
	"path/filepath"
	"strings"
)

// splitPaths normalizes a comma-separated -only list to the slash-relative form
// findings carry, so `./internal/x.go` and `internal/x.go` name the same file.
func splitPaths(list string) map[string]bool {
	out := map[string]bool{}
	for raw := range strings.SplitSeq(list, ",") {
		if p := normalizeOnlyPath(raw); p != "" {
			out[p] = true
		}
	}
	return out
}

func normalizeOnlyPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

// limitToPaths narrows a report to the files a change touched. Repo-wide
// ceilings survive the filter on purpose: a file can push the whole tree past a
// ceiling without exceeding its own budget, and that is the case a narrowed
// check must not hide.
func limitToPaths(over []Finding, overruns []Overrun, paths map[string]bool) ([]Finding, []Overrun) {
	var keptFindings []Finding
	for _, f := range over {
		if paths[normalizeOnlyPath(f.File)] {
			keptFindings = append(keptFindings, f)
		}
	}
	var keptOverruns []Overrun
	for _, o := range overruns {
		if o.File == "" || paths[normalizeOnlyPath(o.File)] {
			keptOverruns = append(keptOverruns, o)
		}
	}
	return keptFindings, keptOverruns
}
