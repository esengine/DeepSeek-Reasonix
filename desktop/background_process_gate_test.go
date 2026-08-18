package main

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestWindowsDesktopCommandsUseProcConstructors keeps new Windows-capable
// desktop code from bypassing internal/proc. Background commands must be
// hidden by default; user-visible launches have to opt in with VisibleCommand.
func TestWindowsDesktopCommandsUseProcConstructors(t *testing.T) {
	roots := []string{".", "../internal"}
	// These packages either are the constructor implementation itself or launch
	// a real user-facing terminal/application from the CLI/desktop launcher.
	visibleOrInfrastructure := []string{
		"../internal/cli/",
		"../internal/desktoplauncher/",
		"../internal/notify/",
		"../internal/proc/",
	}
	windowsBuild := build.Default
	windowsBuild.GOOS = "windows"
	windowsBuild.GOARCH = "amd64"

	checkRoot := func(root string) error {
		return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			cleanPath := filepath.ToSlash(path)
			for _, prefix := range visibleOrInfrastructure {
				if strings.HasPrefix(cleanPath, prefix) {
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if entry.IsDir() {
				if entry.Name() == "third_party" || entry.Name() == "frontend" || entry.Name() == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			dir, name := filepath.Split(path)
			matched, err := windowsBuild.MatchFile(filepath.Clean(dir), name)
			if err != nil || !matched {
				return err
			}

			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			execNames := map[string]bool{}
			for _, imp := range file.Imports {
				importPath, err := strconv.Unquote(imp.Path.Value)
				if err != nil || importPath != "os/exec" {
					continue
				}
				name := "exec"
				if imp.Name != nil {
					name = imp.Name.Name
				}
				execNames[name] = true
			}
			ast.Inspect(file, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok || (selector.Sel.Name != "Command" && selector.Sel.Name != "CommandContext") {
					return true
				}
				ident, ok := selector.X.(*ast.Ident)
				if ok && execNames[ident.Name] {
					t.Errorf("%s bypasses internal/proc with %s.%s", path, ident.Name, selector.Sel.Name)
				}
				return true
			})
			return nil
		})
	}
	for _, root := range roots {
		if err := checkRoot(root); err != nil {
			t.Fatal(err)
		}
	}
}
