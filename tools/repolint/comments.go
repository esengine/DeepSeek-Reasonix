package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

const (
	capDocGoPackage = 40
	capPackageDoc   = 8
	capDeclDoc      = 5
	capFieldDoc     = 3
	capFloating     = 3
)

var (
	bannerRe    = regexp.MustCompile(`^//\s*[-=~*_+#/\x{2500}-\x{257F}]{6,}\s*$`)
	labelledRe  = regexp.MustCompile(`^//\s*[-=~*_+#\x{2500}-\x{257F}]{3,}.*[-=~*_+#\x{2500}-\x{257F}]{3,}\s*$`)
	anchoredRe  = regexp.MustCompile(`\b(TODO|HACK)\(#\d+\)`)
	bareMarkRe  = regexp.MustCompile(`\b(TODO|HACK)\b`)
	fixmeRe     = regexp.MustCompile(`\bFIXME\b`)
	narrativeRe = regexp.MustCompile(`(?i)\b(phase|stage)\s+\d+[a-z]?\d*\b`)
	directiveRe = regexp.MustCompile(`^//(go:|lint:|nolint|export |sys |line )`)
	docCodeRe   = regexp.MustCompile(`^//(\t| {4,})`)
)

func checkComments(s *sourceFile) []Finding {
	var out []Finding
	limits := s.commentLimits()
	preamble := s.cgoPreamble()

	for _, cg := range s.file.Comments {
		if cg == preamble {
			continue
		}
		limit, attached := limits[cg]
		if !attached {
			limit = capFloating
		}
		start, end := s.line(cg.Pos()), s.line(cg.End())
		if n := end - start + 1; n > limit {
			out = append(out, Finding{s.rel, start, ruleEssay,
				fmt.Sprintf("%d-line comment block exceeds the %d-line limit for this position", n, limit), n - limit})
		}
		out = append(out, s.checkCommentText(cg, attached)...)
	}
	return out
}

func (s *sourceFile) commentLimits() map[*ast.CommentGroup]int {
	limits := map[*ast.CommentGroup]int{}
	if s.file.Doc != nil {
		if s.rel == "doc.go" || strings.HasSuffix(s.rel, "/doc.go") {
			limits[s.file.Doc] = capDocGoPackage
		} else {
			limits[s.file.Doc] = capPackageDoc
		}
	}
	ast.Inspect(s.file, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.FuncDecl:
			set(limits, d.Doc, capDeclDoc)
		case *ast.GenDecl:
			set(limits, d.Doc, capDeclDoc)
		case *ast.TypeSpec:
			set(limits, d.Doc, capDeclDoc)
		case *ast.ValueSpec:
			set(limits, d.Doc, capDeclDoc)
		case *ast.Field:
			set(limits, d.Doc, capFieldDoc)
		}
		return true
	})
	return limits
}

// The block comment preceding `import "C"` is compiler input, not prose.
func (s *sourceFile) cgoPreamble() *ast.CommentGroup {
	for _, decl := range s.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT || gen.Doc == nil {
			continue
		}
		for _, spec := range gen.Specs {
			if imp, ok := spec.(*ast.ImportSpec); ok && imp.Path.Value == `"C"` {
				return gen.Doc
			}
		}
	}
	return nil
}

func set(limits map[*ast.CommentGroup]int, cg *ast.CommentGroup, limit int) {
	if cg != nil {
		limits[cg] = limit
	}
}

func (s *sourceFile) checkCommentText(cg *ast.CommentGroup, attached bool) []Finding {
	var out []Finding
	for _, c := range cg.List {
		base, ownsLine := s.line(c.Pos()), !s.trailing(c.Pos())
		// Commented-out code sits in a body or between declarations; a doc
		// comment describing a wire format is prose that happens to parse.
		deadCodeCandidate := ownsLine && !attached
		for i, text := range strings.Split(c.Text, "\n") {
			line, trimmed := base+i, strings.TrimSpace(text)
			if directiveRe.MatchString(trimmed) {
				continue
			}
			switch {
			case bannerRe.MatchString(trimmed), labelledRe.MatchString(trimmed):
				out = append(out, Finding{s.rel, line, ruleBanner, "section-banner separator", 1})
			case deadCodeCandidate && !docCodeRe.MatchString(text) && looksLikeCode(trimmed):
				out = append(out, Finding{s.rel, line, ruleDeadCode, "commented-out code", 1})
			}
			if fixmeRe.MatchString(trimmed) {
				out = append(out, Finding{s.rel, line, ruleMarker, "FIXME is banned: fix it or open an issue and use TODO(#nnn)", 1})
			} else if bareMarkRe.MatchString(trimmed) && !anchoredRe.MatchString(trimmed) {
				out = append(out, Finding{s.rel, line, ruleMarker, "TODO/HACK needs a (#nnn) issue anchor", 1})
			}
			if narrativeRe.MatchString(trimmed) {
				out = append(out, Finding{s.rel, line, ruleNarrative, "phase/stage narrative belongs in the commit message", 1})
			}
		}
	}
	return out
}

func looksLikeCode(text string) bool {
	body := strings.TrimSpace(strings.TrimLeft(text, "/*"))
	body = strings.TrimSuffix(body, "*/")
	if len(body) < 4 || len(body) > 160 {
		return false
	}
	if !strings.Contains(body, ":=") && !strings.ContainsAny(body, "(){};") {
		return false
	}
	_, err := parser.ParseFile(token.NewFileSet(), "", "package p\nfunc _() {\n"+body+"\n}\n", parser.SkipObjectResolution)
	return err == nil
}
