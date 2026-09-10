package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const wailsRuntimeImport = "github.com/wailsapp/wails/v2/pkg/runtime"

// shellFilePatterns name desktop Go files that existed only because of the
// retired Wails, WebView2 or WebKitGTK shell. The delete-shell rules are gone
// from the tree with the old shell; the migrate-host rules keep classifying the
// survivors, and any reintroduced file matching a retired name is flagged
// delete-shell again.
var shellFilePatterns = map[string]struct {
	class class
	owner string
}{
	`^webview2_.*\.go$`:                  {classDeleteShell, "Chromium renderer supervision in the Electron main process"},
	`^webkit_.*\.go$`:                    {classDeleteShell, "WebKitGTK diagnostics are not needed under Chromium"},
	`^linux_renderer_recovery\.go$`:      {classDeleteShell, "Electron render-process-gone recovery"},
	`^nvidia_wayland_linux\.go$`:         {classDeleteShell, "Chromium owns GPU/Wayland selection"},
	`^wails_logger\.go$`:                 {classDeleteShell, "service stderr log captured by the shell supervisor"},
	`^window_restore_diagnostics.*\.go$`: {classDeleteShell, "Electron window presentation has no restore race"},
	`^web_runtime_.*\.go$`:               {classDeleteShell, "renderer identity reported by the shell in hello"},
	`^hang_watchdog.*\.go$`:              {classDeleteShell, "no native UI thread in the Go service"},
	`^icon_repair_.*\.go$`:               {classDeleteShell, "icons are packaged by the Electron bundle"},
	`^window_icon_.*\.go$`:               {classDeleteShell, "icons are packaged by the Electron bundle"},
	`^main\.go$`:                         {classKeepBusiness, "--host-rpc service entry + shell bootstrap"},
	`^single_instance\.go$`:              {classMigrateHost, "Electron requestSingleInstanceLock keyed by canonical home"},
	`^menu\.go$`:                         {classMigrateHost, "Electron application menu"},
	`^tray.*\.go$`:                       {classMigrateHost, "Electron Tray through host/tray.*"},
	`^desktop_shell.*\.go$`:              {classMigrateHost, "coordinator keeps ordering; presentation via host/window.*"},
	`^window_controls\.go$`:              {classMigrateHost, "renderer window controls through the preload"},
	`^window_state\.go$`:                 {classMigrateHost, "geometry persisted by Go, applied through host/window.*"},
	`^zoom_factor\.go$`:                  {classMigrateHost, "zoomFactor in the hello window geometry"},
	`^system_quit.*\.go$`:                {classMigrateHost, "Electron before-quit → desktop/beforeClose"},
	`^relauncher\.go$`:                   {classMigrateHost, "host/app.relaunch"},
	`^superseded_relaunch\.go$`:          {classMigrateHost, "host/app.relaunch"},
	`^app_identity_windows\.go$`:         {classMigrateHost, "Electron app.setAppUserModelId"},
	`^external_opener.*\.go$`:            {classMigrateHost, "openers stay in Go; dialogs via host/dialog.*"},
	`^native_host.*\.go$`:                {classMigrateHost, "nativeHost boundary"},
	`^host_.*\.go$`:                      {classMigrateHost, "hostrpc service mode"},
}

var persistenceLiteralRe = regexp.MustCompile(`\.(json|jsonl|toml|db|lock|sqlite|log|txt)$`)

type goScan struct {
	fset      *token.FileSet
	shellOnly map[string]bool
}

func scanGo(root string, inv *inventory) error {
	dir := filepath.Join(root, "desktop")
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return err
	}
	sort.Strings(names)
	s := &goScan{fset: token.NewFileSet(), shellOnly: map[string]bool{}}
	events := map[string]string{}
	persisted := map[string]string{}
	for _, path := range names {
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(s.fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		rel := "desktop/" + base
		shellClass, shellOwner, isShell := shellFile(base)
		if isShell {
			inv.add(entry{Kind: kindShellFile, Name: rel, Class: shellClass, Owner: shellOwner})
		}
		alias := wailsAlias(file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			s.collectNativeCalls(fn, alias, rel, shellClass, isShell, inv)
			s.collectEvents(fn, events, rel)
			s.collectPersistence(fn, persisted, rel, isShell && shellClass == classDeleteShell)
			if isAppMethod(fn) && fn.Name.IsExported() {
				inv.add(s.commandEntry(fn, alias, rel, shellClass, shellOwner, isShell))
			}
		}
	}
	for name, loc := range events {
		inv.add(entry{Kind: kindEvent, Name: name, Location: loc, Class: classKeepBusiness, Owner: "desktop/event frame (seq + generation), same payload"})
	}
	for name, loc := range persisted {
		e := entry{Kind: kindPersistence, Name: name, Location: loc, Class: classKeepBusiness, Owner: "format unchanged; read by both shells"}
		if s.shellOnly[name] {
			e.Class, e.Owner = classDeleteShell, "no longer written; old files are left in place"
		}
		inv.add(e)
	}
	return nil
}

func shellFile(base string) (class, string, bool) {
	for pattern, rule := range shellFilePatterns {
		if regexp.MustCompile(pattern).MatchString(base) {
			return rule.class, rule.owner, true
		}
	}
	return "", "", false
}

func wailsAlias(file *ast.File) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != wailsRuntimeImport {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "runtime"
	}
	return ""
}

func isAppMethod(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "App"
}

func (s *goScan) commandEntry(fn *ast.FuncDecl, alias, rel string, shellClass class, shellOwner string, isShell bool) entry {
	e := entry{
		Kind:     kindCommand,
		Name:     fn.Name.Name,
		Detail:   signature(fn),
		Location: fmt.Sprintf("%s:%d", rel, s.fset.Position(fn.Pos()).Line),
		Class:    classKeepBusiness,
		Owner:    "hostrpc desktop/invoke",
	}
	switch {
	case isShell && shellClass == classDeleteShell:
		e.Class, e.Owner = classDeleteShell, shellOwner
	case (alias != "" && usesSelector(fn.Body, alias)) || usesNativeHost(fn.Body):
		e.Class, e.Owner = classMigrateHost, "business in Go; native step through nativeHost host/*"
	}
	return e
}

func signature(fn *ast.FuncDecl) string {
	var params, results []string
	for _, f := range fn.Type.Params.List {
		typ := types.ExprString(f.Type)
		if len(f.Names) == 0 {
			params = append(params, typ)
		}
		for _, n := range f.Names {
			params = append(params, n.Name+" "+typ)
		}
	}
	if fn.Type.Results != nil {
		for _, f := range fn.Type.Results.List {
			results = append(results, types.ExprString(f.Type))
		}
	}
	out := "(" + strings.Join(params, ", ") + ")"
	switch len(results) {
	case 0:
	case 1:
		out += " " + results[0]
	default:
		out += " (" + strings.Join(results, ", ") + ")"
	}
	return out
}

func usesSelector(body *ast.BlockStmt, alias string) bool {
	found := false
	if body == nil {
		return false
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == alias {
			found = true
		}
		return true
	})
	return found
}

// usesNativeHost reports a method that reaches the shell through the
// nativeHost boundary once the direct Wails calls have been extracted.
func usesNativeHost(body *ast.BlockStmt) bool {
	found := false
	if body == nil {
		return false
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "nativeHost" {
			found = true
		}
		return true
	})
	return found
}

func (s *goScan) collectNativeCalls(fn *ast.FuncDecl, alias, rel string, shellClass class, isShell bool, inv *inventory) {
	if alias == "" || fn.Body == nil {
		return
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != alias {
			return true
		}
		e := entry{
			Kind:     kindNativeCall,
			Name:     "runtime." + sel.Sel.Name,
			Detail:   "in " + fn.Name.Name,
			Location: fmt.Sprintf("%s:%d", rel, s.fset.Position(call.Pos()).Line),
			Class:    classMigrateHost,
			Owner:    "nativeHost → " + hostMethodFor(sel.Sel.Name),
		}
		if isShell && shellClass == classDeleteShell {
			e.Class, e.Owner = classDeleteShell, "removed with the old shell"
		}
		inv.add(e)
		return true
	})
}

func hostMethodFor(name string) string {
	switch name {
	case "EventsEmit":
		return "desktop/event"
	case "OpenDirectoryDialog", "OpenFileDialog", "OpenMultipleFilesDialog", "SaveFileDialog", "MessageDialog":
		return "host/dialog.*"
	case "BrowserOpenURL":
		return "host/shell.openExternal"
	case "Quit":
		return "host/app.quit"
	case "Hide", "Show":
		return "host/app.hide, host/window.show"
	case "ScreenGetAll":
		return "host/screen.list"
	case "WindowExecJS":
		return "host/remoteWindow.navigate, host/devtools.toggle"
	default:
		return "host/window.*"
	}
}

var eventEmitters = map[string]bool{"emitRuntimeEvent": true, "emitRemoteEvent": true, "EventsEmit": true}

func (s *goScan) collectEvents(fn *ast.FuncDecl, events map[string]string, rel string) {
	if fn.Body == nil {
		return
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		name := calleeName(call.Fun)
		if !eventEmitters[name] {
			return true
		}
		argIndex := 0
		if name == "EventsEmit" {
			argIndex = 1
		}
		if argIndex >= len(call.Args) {
			return true
		}
		if lit, ok := call.Args[argIndex].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			value, err := strconv.Unquote(lit.Value)
			if err == nil {
				if _, seen := events[value]; !seen {
					events[value] = fmt.Sprintf("%s:%d", rel, s.fset.Position(call.Pos()).Line)
				}
			}
		}
		return true
	})
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func (s *goScan) collectPersistence(fn *ast.FuncDecl, persisted map[string]string, rel string, shellOnly bool) {
	if fn.Body == nil {
		return
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || calleeName(call.Fun) != "Join" {
			return true
		}
		for _, arg := range call.Args {
			lit, ok := arg.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil || !persistenceLiteralRe.MatchString(value) || strings.Contains(value, "*") {
				continue
			}
			if _, seen := persisted[value]; !seen {
				persisted[value] = fmt.Sprintf("%s:%d", rel, s.fset.Position(call.Pos()).Line)
				s.shellOnly[value] = shellOnly
			}
		}
		return true
	})
}

func readFile(root, rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
