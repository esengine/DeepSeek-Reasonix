package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(codeIndex{}) }

type codeIndex struct{ workDir string }

func (codeIndex) Name() string { return "code_index" }

func (codeIndex) Description() string {
	return "Lightweight built-in code symbol index. Prefer lsp_* for language semantics and installed code graph MCP tools for call graph, impact, and architecture relationships; use this as the local fallback for file outlines and symbol definition candidates, then verify with read_file or grep."
}

func (codeIndex) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "action":{"type":"string","enum":["outline","search"],"description":"outline lists symbols under path; search finds symbol definition candidates by name."},
  "path":{"type":"string","description":"File or directory path to inspect (default \".\")."},
  "query":{"type":"string","description":"Symbol name or substring for action=search."},
  "kind":{"type":"string","description":"Optional symbol kind filter, such as func, method, class, type, interface, const, var, struct, enum, trait."},
  "limit":{"type":"integer","description":"Maximum symbols to return (default 100, max 200).","minimum":1}
},
"required":["action"]
}`)
}

func (codeIndex) ReadOnly() bool { return true }

const (
	codeIndexDefaultLimit = 100
	codeIndexMaxLimit     = 200
	codeIndexMaxFileSize  = 1 << 20
	codeIndexMaxFiles     = 2000
)

type codeIndexArgs struct {
	Action string `json:"action"`
	Path   string `json:"path"`
	Query  string `json:"query"`
	Kind   string `json:"kind"`
	Limit  int    `json:"limit"`
}

type codeSymbol struct {
	Name      string
	Kind      string
	File      string
	Line      int
	Parent    string
	Signature string
}

func (c codeIndex) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	p := codeIndexArgs{Path: ".", Limit: codeIndexDefaultLimit}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	p.Action = strings.ToLower(strings.TrimSpace(p.Action))
	if p.Action != "outline" && p.Action != "search" {
		return "", fmt.Errorf("action must be outline or search")
	}
	if p.Path == "" {
		p.Path = "."
	}
	if p.Limit <= 0 {
		p.Limit = codeIndexDefaultLimit
	}
	if p.Limit > codeIndexMaxLimit {
		p.Limit = codeIndexMaxLimit
	}
	if p.Action == "search" && strings.TrimSpace(p.Query) == "" {
		return "", fmt.Errorf("query is required for action=search")
	}

	root := resolveIn(c.workDir, p.Path)
	collectionLimit := p.Limit
	if p.Action == "outline" && hasCodeSymbolFilter(p) {
		collectionLimit = 0
	}
	symbols, truncated, err := c.collect(ctx, root, collectionLimit, p.Action == "outline")
	if err != nil {
		return "", err
	}
	symbols = filterCodeSymbols(symbols, p)
	if len(symbols) > p.Limit {
		symbols = symbols[:p.Limit]
		truncated = true
	}
	return formatCodeSymbols(symbols, truncated), nil
}

func (c codeIndex) collect(ctx context.Context, root string, limit int, outline bool) ([]codeSymbol, bool, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, false, fmt.Errorf("code_index %s: %w", root, err)
	}
	var files []string
	if !info.IsDir() {
		if supportedCodeIndexFile(root) {
			files = append(files, root)
		}
	} else {
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				if path != root && skipCodeIndexDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if supportedCodeIndexFile(path) {
				files = append(files, path)
				if len(files) >= codeIndexMaxFiles {
					return filepath.SkipAll
				}
			}
			return nil
		})
		if err != nil {
			return nil, false, fmt.Errorf("code_index walk %s: %w", root, err)
		}
	}
	sort.Strings(files)

	var symbols []codeSymbol
	truncated := len(files) >= codeIndexMaxFiles
	for _, file := range files {
		if ctx.Err() != nil {
			return nil, truncated, ctx.Err()
		}
		found, err := c.parseFile(file)
		if err != nil {
			continue
		}
		symbols = append(symbols, found...)
		if outline && limit > 0 && len(symbols) >= limit {
			truncated = true
			break
		}
	}
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].File != symbols[j].File {
			return symbols[i].File < symbols[j].File
		}
		if symbols[i].Line != symbols[j].Line {
			return symbols[i].Line < symbols[j].Line
		}
		return symbols[i].Name < symbols[j].Name
	})
	return symbols, truncated, nil
}

func (c codeIndex) parseFile(path string) ([]codeSymbol, error) {
	if info, err := os.Stat(path); err != nil || info.Size() > codeIndexMaxFileSize {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("file too large")
	}
	if filepath.Ext(path) == ".go" {
		return c.parseGo(path)
	}
	return c.parseText(path)
}

func (c codeIndex) parseGo(path string) ([]codeSymbol, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []codeSymbol
	add := func(name, kind string, pos token.Pos, parent, sig string) {
		if name == "" {
			return
		}
		out = append(out, codeSymbol{
			Name:      name,
			Kind:      kind,
			File:      c.displayPath(path),
			Line:      fset.Position(pos).Line,
			Parent:    parent,
			Signature: sig,
		})
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			parent := ""
			kind := "func"
			if d.Recv != nil && len(d.Recv.List) > 0 {
				parent = exprName(d.Recv.List[0].Type)
				kind = "method"
			}
			add(d.Name.Name, kind, d.Name.Pos(), parent, goFuncSignature(d, parent))
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					kind := "type"
					switch s.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					add(s.Name.Name, kind, s.Name.Pos(), "", kind+" "+s.Name.Name)
				case *ast.ValueSpec:
					kind := strings.ToLower(d.Tok.String())
					for _, name := range s.Names {
						add(name.Name, kind, name.Pos(), "", kind+" "+name.Name)
					}
				}
			}
		}
	}
	return out, nil
}

func (c codeIndex) parseText(path string) ([]codeSymbol, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []codeSymbol
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		for _, m := range codeIndexMatchers(filepath.Ext(path)) {
			if sym, ok := m.match(text); ok {
				sym.File = c.displayPath(path)
				sym.Line = line
				out = append(out, sym)
				break
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (c codeIndex) displayPath(path string) string {
	if c.workDir != "" {
		if rel, err := filepath.Rel(c.workDir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func hasCodeSymbolFilter(p codeIndexArgs) bool {
	return strings.TrimSpace(p.Kind) != "" || strings.TrimSpace(p.Query) != ""
}

func filterCodeSymbols(in []codeSymbol, p codeIndexArgs) []codeSymbol {
	query := strings.ToLower(strings.TrimSpace(p.Query))
	kind := strings.ToLower(strings.TrimSpace(p.Kind))
	out := make([]codeSymbol, 0, len(in))
	for _, s := range in {
		if kind != "" && strings.ToLower(s.Kind) != kind {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(s.Name + " " + s.Parent + " " + s.Signature)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

func formatCodeSymbols(symbols []codeSymbol, truncated bool) string {
	if len(symbols) == 0 {
		return "(no symbols)"
	}
	var b strings.Builder
	for _, s := range symbols {
		name := s.Name
		if s.Parent != "" {
			name = s.Parent + "." + name
		}
		if s.Signature != "" {
			fmt.Fprintf(&b, "%s:%d: %s %s — %s\n", s.File, s.Line, s.Kind, name, s.Signature)
		} else {
			fmt.Fprintf(&b, "%s:%d: %s %s\n", s.File, s.Line, s.Kind, name)
		}
	}
	if truncated {
		b.WriteString("... (truncated; narrow path/query/kind or raise limit)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func supportedCodeIndexFile(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".py", ".java", ".kt", ".kts", ".cs", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp":
		return true
	default:
		return false
	}
}

func skipCodeIndexDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "__pycache__", ".idea", ".vscode", ".next", "dist", "build", "target", "coverage":
		return true
	default:
		return false
	}
}

func exprName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprName(t.X)
	case *ast.SelectorExpr:
		return exprName(t.X) + "." + t.Sel.Name
	default:
		return ""
	}
}

func goFuncSignature(fn *ast.FuncDecl, parent string) string {
	if parent == "" {
		return "func " + fn.Name.Name
	}
	return "func (" + parent + ") " + fn.Name.Name
}

type codeIndexMatcher struct {
	kind      string
	re        *regexp.Regexp
	nameGroup int
	kindGroup int
}

func (m codeIndexMatcher) match(line string) (codeSymbol, bool) {
	match := m.re.FindStringSubmatch(line)
	if match == nil || m.nameGroup >= len(match) {
		return codeSymbol{}, false
	}
	name := strings.TrimSpace(match[m.nameGroup])
	if name == "" {
		return codeSymbol{}, false
	}
	kind := m.kind
	if m.kindGroup > 0 && m.kindGroup < len(match) && strings.TrimSpace(match[m.kindGroup]) != "" {
		kind = normalizeCodeIndexKind(match[m.kindGroup])
	}
	return codeSymbol{Name: name, Kind: kind, Signature: strings.TrimSpace(line)}, true
}

func normalizeCodeIndexKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "function":
		return "func"
	default:
		return kind
	}
}

var (
	rePyClass     = regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	rePyFunc      = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reJSClass     = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	reJSFunc      = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
	reJSInterface = regexp.MustCompile(`^\s*(?:export\s+)?interface\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	reJSType      = regexp.MustCompile(`^\s*(?:export\s+)?type\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	reJSEnum      = regexp.MustCompile(`^\s*(?:export\s+)?enum\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	reJSArrow     = regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_$][A-Za-z0-9_$]*)\s*=>`)
	reJavaType    = regexp.MustCompile(`^\s*(?:public|protected|private|abstract|final|static|sealed|data|\s)*\s*(class|interface|enum|record|object)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	reJavaMeth    = regexp.MustCompile(`^\s*(?:public|protected|private|static|final|abstract|synchronized|native|\s)+[\w<>\[\], ?]+\s+([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*\)\s*(?:\{|$)`)
	reRustItem    = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(fn|struct|enum|trait)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	reCFunc       = regexp.MustCompile(`^\s*(?:[\w:*&<>\[\],]+\s+)+([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*\)\s*(?:\{|$)`)
)

func codeIndexMatchers(ext string) []codeIndexMatcher {
	switch ext {
	case ".py":
		return []codeIndexMatcher{{kind: "class", re: rePyClass, nameGroup: 1}, {kind: "func", re: rePyFunc, nameGroup: 1}}
	case ".js", ".jsx", ".ts", ".tsx":
		return []codeIndexMatcher{
			{kind: "class", re: reJSClass, nameGroup: 1},
			{kind: "func", re: reJSFunc, nameGroup: 1},
			{kind: "interface", re: reJSInterface, nameGroup: 1},
			{kind: "type", re: reJSType, nameGroup: 1},
			{kind: "enum", re: reJSEnum, nameGroup: 1},
			{kind: "func", re: reJSArrow, nameGroup: 1},
		}
	case ".java", ".kt", ".kts", ".cs":
		return []codeIndexMatcher{{re: reJavaType, nameGroup: 2, kindGroup: 1}, {kind: "method", re: reJavaMeth, nameGroup: 1}}
	case ".rs":
		return []codeIndexMatcher{{re: reRustItem, nameGroup: 2, kindGroup: 1}}
	case ".c", ".cc", ".cpp", ".h", ".hpp":
		return []codeIndexMatcher{{kind: "func", re: reCFunc, nameGroup: 1}}
	default:
		return nil
	}
}
