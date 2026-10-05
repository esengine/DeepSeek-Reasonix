package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	controlPkgPath = "reasonix/internal/session/control"
	controllerName = "Controller"
	modulePath     = "reasonix"
)

// One declared build target keeps the matrix off the machine that reads it:
// internal/frontend/cli carries _windows and _unix files, and a GOOS-dependent file set
// would put a developer's baseline at odds with CI. This is the CLI's own
// release configuration, so the tree has to keep it building anyway.
var parityTarget = []string{"CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64"}

const parityArch = "amd64"

type listedPkg struct {
	ImportPath string
	Export     string
	Dir        string
	GoFiles    []string
}

// loadParityMatrix reads the matrix from type information and never from
// selector names: Close, Status and Label are controller capabilities and also
// methods on half the standard library, so a name match would record
// os.File.Close as serve driving Lifecycle.Close — a false "wired" that hides
// real debt, the one direction a ratchet must not fail in.
func loadParityMatrix(root string, ports []frontendPort) (parityMatrix, error) {
	pkgs, err := listPackages(root, ports)
	if err != nil {
		return parityMatrix{}, err
	}
	exports := map[string]string{}
	for path, p := range pkgs {
		if p.Export != "" {
			exports[path] = p.Export
		}
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	})
	control, err := imp.Import(controlPkgPath)
	if err != nil {
		return parityMatrix{}, fmt.Errorf("load %s: %w", controlPkgPath, err)
	}
	caps, err := controllerCapabilities(root, fset, control)
	if err != nil {
		return parityMatrix{}, err
	}
	m := parityMatrix{
		capabilities: caps,
		reachable:    map[string]map[string]bool{},
		consumed:     map[string]map[string]bool{},
	}
	for _, port := range ports {
		if m.reachable[port.pkg], err = portMethods(control, port.port); err != nil {
			return parityMatrix{}, err
		}
		pkg, ok := pkgs[modulePath+"/"+port.pkg]
		if !ok {
			return parityMatrix{}, fmt.Errorf("frontendPorts names %s, which go list did not report", port.pkg)
		}
		if m.consumed[port.pkg], err = controlCalls(fset, imp, pkg); err != nil {
			return parityMatrix{}, err
		}
	}
	return m, nil
}

// listPackages resolves every frontend and everything it imports to compiled
// export data, which is what makes the type check cheap: the build cache has
// already done the work, and a warm tree answers in well under a second.
func listPackages(root string, ports []frontendPort) (map[string]listedPkg, error) {
	args := []string{"list", "-export", "-deps", "-json=ImportPath,Export,Dir,GoFiles"}
	for _, p := range ports {
		args = append(args, "./"+p.pkg)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), parityTarget...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("frontend-parity reads types and needs a building tree; `go list -export` failed: %w\n%s", err, stderr.String())
	}
	pkgs := map[string]listedPkg{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p listedPkg
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		pkgs[p.ImportPath] = p
	}
	return pkgs, nil
}

func controllerCapabilities(root string, fset *token.FileSet, control *types.Package) ([]capability, error) {
	obj := control.Scope().Lookup(controllerName)
	if obj == nil {
		return nil, fmt.Errorf("%s declares no %s", controlPkgPath, controllerName)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var out []capability
	for sel := range types.NewMethodSet(types.NewPointer(obj.Type())).Methods() {
		fn := sel.Obj()
		if !fn.Exported() {
			continue
		}
		pos := fset.Position(fn.Pos())
		if !fn.Pos().IsValid() || pos.Filename == "" {
			return nil, fmt.Errorf("export data carries no position for %s.%s", controllerName, fn.Name())
		}
		out = append(out, capability{name: fn.Name(), file: relSlash(abs, pos.Filename), line: pos.Line})
	}
	return out, nil
}

func relSlash(abs, path string) string {
	rel, err := filepath.Rel(abs, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// portMethods is the port's complete method set, so a capability reached
// through an embedded sub-port counts exactly like one named directly.
func portMethods(control *types.Package, name string) (map[string]bool, error) {
	obj := control.Scope().Lookup(name)
	if obj == nil {
		return nil, fmt.Errorf("%s declares no port %s", controlPkgPath, name)
	}
	iface, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return nil, fmt.Errorf("%s.%s is not an interface", controlPkgPath, name)
	}
	out := map[string]bool{}
	for m := range iface.Methods() {
		out[m.Name()] = true
	}
	return out, nil
}

// controlCalls type-checks one frontend and reports the control methods it
// selects. Test files are left out: a call that exists only in a test is the
// corpse-warming orphan.go already refuses to count as a caller.
func controlCalls(fset *token.FileSet, imp types.Importer, pkg listedPkg) (map[string]bool, error) {
	files := make([]*ast.File, 0, len(pkg.GoFiles))
	for _, name := range pkg.GoFiles {
		file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		files = append(files, file)
	}
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{Importer: imp, Sizes: types.SizesFor("gc", parityArch)}
	if _, err := conf.Check(pkg.ImportPath, fset, files, info); err != nil {
		return nil, fmt.Errorf("type-check %s: %w", pkg.ImportPath, err)
	}
	out := map[string]bool{}
	for _, sel := range info.Selections {
		fn, ok := sel.Obj().(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != controlPkgPath {
			continue
		}
		out[fn.Name()] = true
	}
	return out, nil
}
