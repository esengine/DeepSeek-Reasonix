package hostrpc

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// CommandOwnership records source provenance, not a second authorization
// policy. The existing owner resolves scope and validates all wire inputs.
type CommandOwnership struct {
	Domain  string       `json:"domain,omitempty"`
	Owner   string       `json:"owner,omitempty"`
	Sources []string     `json:"sources,omitempty"`
	Scope   CommandScope `json:"scope"`
}

type CommandScope struct {
	Kind     string   `json:"kind"`
	Resolver string   `json:"resolver"`
	Inputs   []string `json:"inputs"`
}

// SourceOwnership scans all platform declarations, independent of build tags.
// It uses repository-relative filenames and named inputs, never source line
// numbers, absolute paths or a guess based on the command's name.
func SourceOwnership(dir, target string) (map[string]CommandOwnership, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	owners := map[string]CommandOwnership{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, err
		}
		contextAlias := ""
		for _, imp := range file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == "context" {
				contextAlias = "context"
				if imp.Name != nil {
					contextAlias = imp.Name.Name
				}
			}
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !fn.Name.IsExported() {
				continue
			}
			receiver := fn.Recv.List[0].Type
			if ptr, ok := receiver.(*ast.StarExpr); ok {
				receiver = ptr.X
			}
			id, ok := receiver.(*ast.Ident)
			if !ok || id.Name != target {
				continue
			}
			inputs := sourceWireInputs(fn.Type.Params, contextAlias)
			owner := target + "." + fn.Name.Name
			kind := "owner-inputs"
			if len(inputs) == 0 {
				kind = "owner-state"
			}
			source := "desktop/" + name
			metadata, exists := owners[fn.Name.Name]
			if exists {
				if !slices.Equal(metadata.Scope.Inputs, inputs) {
					return nil, fmt.Errorf("%s: platform declarations disagree on wire inputs", owner)
				}
				metadata.Sources = append(metadata.Sources, source)
			} else {
				metadata = CommandOwnership{Owner: owner, Sources: []string{source}, Scope: CommandScope{Kind: kind, Resolver: owner, Inputs: inputs}}
			}
			// A domain is the complete set of actual source modules. Platform
			// alternatives are kept together, so every platform has one digest.
			slices.Sort(metadata.Sources)
			metadata.Domain = strings.Join(metadata.Sources, "|")
			owners[fn.Name.Name] = metadata
		}
	}
	if len(owners) == 0 {
		return nil, fmt.Errorf("no %s commands in %s", target, dir)
	}
	return owners, nil
}

func sourceWireInputs(params *ast.FieldList, contextAlias string) []string {
	inputs := []string{}
	for index, field := range params.List {
		if sel, ok := field.Type.(*ast.SelectorExpr); ok && index == 0 && len(field.Names) <= 1 {
			if pkg, ok := sel.X.(*ast.Ident); ok && contextAlias != "" && pkg.Name == contextAlias && sel.Sel.Name == "Context" {
				continue
			}
		}
		if len(field.Names) == 0 {
			inputs = append(inputs, fmt.Sprintf("arg%d", len(inputs)))
			continue
		}
		for _, param := range field.Names {
			inputs = append(inputs, param.Name)
		}
	}
	return inputs
}
