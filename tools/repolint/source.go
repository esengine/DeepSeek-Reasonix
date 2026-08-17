package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// wailsjs holds Wails-generated Go bindings and is gitignored: measuring it
// reports debt against a build product nobody can edit, and its presence
// depends on whether a desktop build has run locally.
var skipDirs = map[string]bool{
	"node_modules": true,
	"third_party":  true,
	"vendor":       true,
	"testdata":     true,
	"dist":         true,
	"bin":          true,
	"wailsjs":      true,
}

var generatedRe = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

type sourceFile struct {
	rel   string
	fset  *token.FileSet
	file  *ast.File
	src   []string
	lines int
}

// A comment sharing its line with code annotates that code; only a comment
// that owns its line can be commented-out code.
func (s *sourceFile) trailing(pos token.Pos) bool {
	p := s.fset.Position(pos)
	if p.Line < 1 || p.Line > len(s.src) || p.Column < 2 {
		return false
	}
	line := s.src[p.Line-1]
	if p.Column-1 > len(line) {
		return false
	}
	return strings.TrimSpace(line[:p.Column-1]) != ""
}

func collect(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !lintable(name) || strings.Contains(name, "_generated.") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

func parseSource(root, rel string) (*sourceFile, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	return parseBytes(rel, data), nil
}

// lintable reports whether repolint reads this file at all. Go files take every
// rule; TypeScript files take only the ones expressed on raw lines, since the
// rest are written against the Go AST.
func lintable(name string) bool {
	return strings.HasSuffix(name, ".go") ||
		strings.HasSuffix(name, ".ts") ||
		strings.HasSuffix(name, ".tsx")
}

func parseBytes(rel string, data []byte) *sourceFile {
	if isGenerated(data) {
		return nil
	}
	lines := splitLines(data)
	if !strings.HasSuffix(rel, ".go") {
		// No fset/file: run() gives these the line-based rules only.
		return &sourceFile{rel: rel, src: lines, lines: len(lines)}
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, data, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	return &sourceFile{rel: rel, fset: fset, file: file, src: lines, lines: len(lines)}
}

func splitLines(data []byte) []string {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

func isGenerated(data []byte) bool {
	for i, line := range strings.SplitN(string(data), "\n", 12) {
		if i >= 11 {
			break
		}
		if generatedRe.MatchString(strings.TrimRight(line, "\r")) {
			return true
		}
	}
	return false
}

func (s *sourceFile) isTest() bool {
	return strings.HasSuffix(s.rel, "_test.go") ||
		strings.Contains(s.rel, "/__tests__/") ||
		strings.Contains(s.rel, ".test.") ||
		strings.Contains(s.rel, ".spec.")
}

func (s *sourceFile) line(p token.Pos) int { return s.fset.Position(p).Line }

type importRef struct {
	path string
	line int
}

func (s *sourceFile) importRefs() []importRef {
	if s.isTest() || s.file == nil {
		return nil
	}
	out := make([]importRef, 0, len(s.file.Imports))
	for _, spec := range s.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		out = append(out, importRef{path: path, line: s.line(spec.Pos())})
	}
	return out
}
