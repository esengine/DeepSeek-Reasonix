package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/diff"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(deleteSymbol{}) }

type deleteSymbol struct {
	roots   []string
	guard   SessionDataGuard
	managed ManagedConfigPaths
	workDir string
}

type symbolMatch struct {
	name     string
	kind     string
	parent   string
	line     int
	start    token.Pos
	docStart token.Pos // start of the symbol's doc comment, if any (excluded from start)
	end      token.Pos
	siblings []string
}

func (deleteSymbol) Name() string { return "delete_symbol" }

func (deleteSymbol) Description() string {
	return "Delete a named symbol (function, method, type, interface, const, var) from a Go source file using AST parsing. For non-Go files, use delete_range with manual anchors."
}

func (deleteSymbol) Schema() json.RawMessage {
	return json.RawMessage(`{
	"type":"object",
	"properties":{
		"path":{"type":"string","description":"Path to the source file"},
		"name":{"type":"string","description":"Name of the symbol to delete"},
		"kind":{"type":"string","description":"Optional kind filter: func, method, type, interface, const, var"},
		"parent":{"type":"string","description":"Optional parent struct name for method disambiguation"}
	},
	"required":["path","name"]
}`)
}

func (deleteSymbol) ReadOnly() bool { return false }

func (d deleteSymbol) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path   string `json:"path"`
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		Parent string `json:"parent"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if p.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	p.Path = resolveIn(d.workDir, p.Path)
	if err := confineWrite(ctx, d.roots, d.guard, d.managed, p.Path); err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(p.Path))
	if ext != ".go" {
		return "", fmt.Errorf("delete_symbol only supports Go files — use delete_range for %s files", ext)
	}

	m, fset, err := d.findSymbol(p.Path, p.Name, p.Kind, p.Parent)
	if err != nil {
		return "", err
	}

	src, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}
	original := string(src)

	newContent := deleteLines(original, fset, m)
	if err := os.WriteFile(p.Path, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}

	change := diff.Build(p.Path, original, newContent, diff.Modify)
	return change.Diff, nil
}

func (d deleteSymbol) Preview(args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path   string `json:"path"`
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		Parent string `json:"parent"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return diff.Change{}, fmt.Errorf("path is required")
	}
	if p.Name == "" {
		return diff.Change{}, fmt.Errorf("name is required")
	}
	p.Path = resolveIn(d.workDir, p.Path)
	if err := confinePreview(d.roots, d.guard, d.managed, p.Path); err != nil {
		return diff.Change{}, err
	}

	ext := strings.ToLower(filepath.Ext(p.Path))
	if ext != ".go" {
		return diff.Change{}, fmt.Errorf("delete_symbol only supports Go files")
	}

	m, fset, err := d.findSymbol(p.Path, p.Name, p.Kind, p.Parent)
	if err != nil {
		return diff.Change{}, err
	}

	src, err := os.ReadFile(p.Path)
	if err != nil {
		return diff.Change{}, fmt.Errorf("read %s: %w", p.Path, err)
	}
	original := string(src)

	newContent := deleteLines(original, fset, m)
	return diff.Build(p.Path, original, newContent, diff.Modify), nil
}

func (d deleteSymbol) findSymbol(path, name, kind, parent string) (symbolMatch, *token.FileSet, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return symbolMatch{}, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	matches := collectSymbols(fset, f)

	var byName []symbolMatch
	for _, m := range matches {
		if m.name == name {
			byName = append(byName, m)
		}
	}
	if len(byName) == 0 {
		return symbolMatch{}, nil, fmt.Errorf("symbol %q not found in %s", name, path)
	}

	filtered := byName
	if kind != "" {
		var byKind []symbolMatch
		for _, m := range filtered {
			if m.kind == kind {
				byKind = append(byKind, m)
			}
		}
		if len(byKind) == 0 {
			return symbolMatch{}, nil, fmt.Errorf("symbol %q with kind %q not found", name, kind)
		}
		filtered = byKind
	}
	if parent != "" {
		var byParent []symbolMatch
		for _, m := range filtered {
			if m.parent == parent {
				byParent = append(byParent, m)
			}
		}
		if len(byParent) == 0 {
			return symbolMatch{}, nil, fmt.Errorf("symbol %q (kind=%q parent=%q) not found", name, kind, parent)
		}
		filtered = byParent
	}

	if len(filtered) > 1 {
		var b strings.Builder
		fmt.Fprintf(&b, "Multiple matches for %q — disambiguate with kind/parent:\n", name)
		for _, m := range filtered {
			fmt.Fprintf(&b, "  line %d: %s %s", m.line, m.kind, m.name)
			if m.parent != "" {
				fmt.Fprintf(&b, " (on %s)", m.parent)
			}
			b.WriteString("\n")
		}
		return symbolMatch{}, nil, fmt.Errorf("%s", b.String())
	}

	if len(filtered[0].siblings) > 1 {
		return symbolMatch{}, nil, fmt.Errorf("%s %q is declared in a multi-name %s spec with %s; delete_symbol refuses to remove it because that would also delete sibling symbols", filtered[0].kind, name, filtered[0].kind, strings.Join(filtered[0].siblings, ", "))
	}

	return filtered[0], fset, nil
}

func collectSymbols(fset *token.FileSet, f *ast.File) []symbolMatch {
	var matches []symbolMatch
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			m := symbolMatch{
				name:  d.Name.Name,
				kind:  "func",
				start: d.Pos(),
				end:   d.End(),
				line:  fset.Position(d.Pos()).Line,
			}
			if d.Doc != nil {
				m.docStart = d.Doc.Pos()
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				m.kind = "method"
				recvType := d.Recv.List[0].Type
				if se, ok := recvType.(*ast.StarExpr); ok {
					if ident, ok := se.X.(*ast.Ident); ok {
						m.parent = ident.Name
					}
				} else if ident, ok := recvType.(*ast.Ident); ok {
					m.parent = ident.Name
				}
			}
			matches = append(matches, m)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					m := symbolMatch{
						name:  s.Name.Name,
						start: s.Pos(),
						end:   s.End(),
						line:  fset.Position(s.Pos()).Line,
					}
					if _, ok := s.Type.(*ast.InterfaceType); ok {
						m.kind = "interface"
					} else {
						m.kind = "type"
					}
					if doc := specDoc(d, s.Doc); doc != nil {
						m.docStart = doc.Pos()
					}
					matches = append(matches, m)
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					names := make([]string, 0, len(s.Names))
					for _, ident := range s.Names {
						names = append(names, ident.Name)
					}
					var docStart token.Pos
					if doc := specDoc(d, s.Doc); doc != nil {
						docStart = doc.Pos()
					}
					for _, ident := range s.Names {
						matches = append(matches, symbolMatch{
							name:     ident.Name,
							kind:     kind,
							start:    ident.Pos(),
							docStart: docStart,
							end:      s.End(), // whole spec, incl. a multi-line value — ident.End() stops at the name
							line:     fset.Position(ident.Pos()).Line,
							siblings: names,
						})
					}
				}
			}
		}
	}
	return matches
}

// specDoc returns the doc comment governing one spec of a GenDecl: the spec's own
// doc when grouped (type/const/var (...)), else the GenDecl's doc for an
// unparenthesized single declaration — where the parser attaches the comment to
// the GenDecl, not the spec. nil for an undocumented spec, and never the group's
// own doc when deleting just one spec of a parenthesized block.
func specDoc(gen *ast.GenDecl, own *ast.CommentGroup) *ast.CommentGroup {
	if own != nil {
		return own
	}
	if gen.Lparen == token.NoPos {
		return gen.Doc
	}
	return nil
}

func deleteLines(content string, fset *token.FileSet, m symbolMatch) string {
	start := m.start
	if m.docStart.IsValid() {
		start = m.docStart // delete the doc comment along with the symbol, not orphan it
	}
	startOff := fset.Position(start).Offset
	endOff := fset.Position(m.end).Offset

	lineStart := startOff
	for lineStart > 0 && content[lineStart-1] != '\n' {
		lineStart--
	}

	lineEnd := endOff
	for lineEnd < len(content) && content[lineEnd] != '\n' {
		lineEnd++
	}
	if lineEnd < len(content) {
		lineEnd++
	}

	return content[:lineStart] + content[lineEnd:]
}
