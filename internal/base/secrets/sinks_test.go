package secrets

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialSinkRegistry(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	registry := map[string][]string{
		"internal/ext/mcpsetup/secrets.go:Redact":                                         {"RedactConfigValue"},
		"internal/ext/mcpsetup/secrets.go:RedactURL":                                      {"RedactEndpoint"},
		"internal/ext/installsource/types.go:publicActions":                               {"RedactEndpoint", "RedactConfigMap", "RedactArgs"},
		"internal/ext/installsource/mcp.go:mcpActionRisk":                                 {"RedactEndpoint", "CredentialKey", "CredentialValue"},
		"internal/ext/installsource/marketplace_object_source.go:marketplaceObjectSource": {"RedactEndpoint"},
		"internal/ext/pluginpkg/export.go:stripNode":                                      {"RedactEndpoint", "RedactArgs", "RedactConfigValue"},
		"internal/ext/pluginpkg/inventory_mcp.go:mcpServerRefs":                           {"RedactEndpoint", "RedactConfigValue"},
		"internal/frontend/cli/mcp.go:mcpList":                                            {"redactMCPURL", "RedactArgs"},
		"internal/frontend/cli/mcp.go:printMCPEntry":                                      {"redactMCPURL", "redactMCPConfigValue", "RedactArgs"},
		"internal/frontend/serve/catalog.go:mcpParse":                                     {"RedactURL", "RedactConfigMap", "RedactArgs"},
	}
	seen := map[string]bool{}
	projectedSinks := map[string]bool{"internal/frontend/cli/plugin.go:printPluginInventory": true}
	operationalFormatters := map[string]bool{"internal/frontend/serve/provider_edit.go:assemblyShape": true}
	for _, dir := range []string{"internal/ext/mcpsetup", "internal/ext/installsource", "internal/ext/pluginpkg", "internal/frontend/cli", "internal/frontend/serve", "internal/ext/plugin"} {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(dir)), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil {
					continue
				}
				id := filepath.ToSlash(rel) + ":" + function.Name.Name
				sources := sinkSources(function.Body)
				calls := map[string]bool{}
				ast.Inspect(function.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					name := sinkCallName(call.Fun)
					calls[name] = true
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
						if owner, ok := selector.X.(*ast.Ident); ok && owner.Name == "secrets" && (name == "RedactCredentials" || name == "RedactError") && (strings.HasPrefix(dir, "internal/ext/plugin") || dir == "internal/ext/installsource") {
							t.Errorf("MCP diagnostic sink %s uses a text scrubber", id)
						}
					}
					if !projectedSinks[id] && !operationalFormatters[id] && (name == "Printf" || name == "Sprintf" || name == "Errorf" || name == "Warn" || name == "Info") {
						for _, arg := range call.Args {
							if credentialSource(arg, sources) {
								t.Errorf("unprojected credential sink %s: %s", id, name)
							}
						}
					}
					return true
				})
				if required, ok := registry[id]; ok {
					seen[id] = true
					for _, call := range required {
						if !calls[call] {
							t.Errorf("sink %s bypasses %s", id, call)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for id := range registry {
		if !seen[id] {
			t.Errorf("sink missing from registry scan: %s", id)
		}
	}
}

func sinkCallName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.SelectorExpr:
		return expr.Sel.Name
	case *ast.Ident:
		return expr.Name
	}
	return ""
}

func rawCredentialField(expr ast.Expr) bool {
	return credentialSource(expr, nil)
}

func credentialSource(expr ast.Expr, sources map[string]bool) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			switch sinkCallName(call.Fun) {
			case "RedactURL", "RedactEndpoint", "RedactConfigValue", "RedactConfigMap", "RedactArgs", "redactMCPURL", "redactMCPConfigValue", "Redact", "DiagnosticError", "len", "cap", "sortedMapKeys":
				return false
			}
		}
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if selector.Sel.Name == "Path" {
				return false
			}
			switch selector.Sel.Name {
			case "URL", "Headers", "Env", "getURL", "url":
				found = true
			}
			return false
		}
		if composite, ok := node.(*ast.CompositeLit); ok {
			for _, element := range composite.Elts {
				found = found || credentialSource(element, sources)
			}
			return false
		}
		if identifier, ok := node.(*ast.Ident); ok && sources[identifier.Name] {
			found = true
		}
		return true
	})
	return found
}

func sinkSources(body *ast.BlockStmt) map[string]bool {
	sources := map[string]bool{}
	for range 3 {
		ast.Inspect(body, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.AssignStmt:
				for i, value := range statement.Rhs {
					if call, ok := value.(*ast.CallExpr); ok && !sourceTransform(call) {
						continue
					}
					if i < len(statement.Lhs) && credentialSource(value, sources) {
						if name, ok := statement.Lhs[i].(*ast.Ident); ok {
							sources[name.Name] = true
						}
					}
				}
			case *ast.RangeStmt:
				if credentialSource(statement.X, sources) {
					if name, ok := statement.Value.(*ast.Ident); ok {
						sources[name.Name] = true
					}
				}
			}
			return true
		})
	}
	return sources
}

func sourceTransform(call *ast.CallExpr) bool {
	switch sinkCallName(call.Fun) {
	case "TrimSpace", "Join", "String", "Sprintf", "ToLower", "ToUpper", "ReplaceAll":
		return true
	}
	return false
}
