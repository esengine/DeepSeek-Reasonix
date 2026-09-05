package main

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"
)

// The frontmatter document is canonical and the flat map is a lossy adapter
// over it, so the adapter's callers are a closed list. Asking instead whether a
// read is "new" would judge intent, which nothing in the source records.
var flatViewReaders = []string{
	"internal/frontmatter/frontmatter.go",    // SplitLegacy, its one wrapper
	"internal/command/command.go",            // slash-command vocabulary
	"internal/installsource/skill.go",        // install-time skill sniffing
	"internal/memory/store.go",               // memory note vocabulary
	"internal/outputstyle/outputstyle.go",    // output-style vocabulary
	"internal/pluginpkg/claude_compat.go",    // imported Claude packages
	"internal/pluginpkg/pluginpkg.go",        // plugin manifest vocabulary
	"internal/skill/builtincontent/embed.go", // shipped built-in bodies
	"internal/skill/profile.go",              // profile vocabulary
	"internal/skill/skill.go",                // the 17-key skill vocabulary
}

// flatViewCalls are the two entry points into the lossy view, named so a
// selector match is unambiguous: neither occurs in the standard library, so
// this rule never guesses what a bare Flat or Split meant.
var flatViewCalls = map[string]bool{"SplitLegacy": true, "LegacyFlat": true}

// checkFlatView holds the legacy frontmatter view to its declared readers. A
// vocabulary that migrates to the structured document comes off the list, so
// the boundary only ever tightens.
func checkFlatView(s *sourceFile) []Finding {
	if strings.HasSuffix(s.rel, "_test.go") || flatViewAllowed(s.rel) {
		return nil
	}
	return checkFlatViewIgnoringList(s)
}

// checkFlatViewIgnoringList reports the reads themselves. The list's own test
// uses it to prove every declared reader still needs its exemption.
func checkFlatViewIgnoringList(s *sourceFile) []Finding {
	var out []Finding
	ast.Inspect(s.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !flatViewCalls[sel.Sel.Name] {
			return true
		}
		out = append(out, Finding{
			File: s.rel,
			Line: s.fset.Position(call.Pos()).Line,
			Rule: ruleFlatView,
			Msg: fmt.Sprintf("%s reads the lossy frontmatter view, which drops the key a field was written under. "+
				"Read frontmatter.Parse and Document.Lookup/Has instead; the flat view's readers are the list in tools/repolint/flatview.go", sel.Sel.Name),
			Weight: 1,
		})
		return true
	})
	return out
}

func flatViewAllowed(rel string) bool {
	return slices.Contains(flatViewReaders, strings.ReplaceAll(rel, "\\", "/"))
}
