package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/mod/modfile"
)

const (
	webview2ModulePath = "github.com/wailsapp/go-webview2"
	webview2PatchPath  = "./third_party/go-webview2"
)

func TestWebView2PatchWiring(t *testing.T) {
	modData, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.Parse("go.mod", modData, nil)
	if err != nil {
		t.Fatal(err)
	}
	replaced := false
	for _, directive := range mod.Replace {
		if directive.Old.Path == webview2ModulePath && directive.New.Path == webview2PatchPath {
			replaced = true
			break
		}
	}
	if !replaced {
		t.Fatalf("%s must be replaced by %s", webview2ModulePath, webview2PatchPath)
	}

	patchFile := filepath.Join("third_party", "go-webview2", "pkg", "edge", "chromium.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), patchFile, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	policyEnabled := false
	policyApplied := false
	proxyIsolationArgDefined := false
	proxyIsolationArgApplied := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.ValueSpec:
			if len(value.Names) == 1 && value.Names[0].Name == "shouldDetectMonitorScaleChanges" && len(value.Values) == 1 {
				ident, ok := value.Values[0].(*ast.Ident)
				policyEnabled = ok && ident.Name == "true"
			}
			if len(value.Names) == 1 && value.Names[0].Name == "reasonixNoProxyServerBrowserArg" && len(value.Values) == 1 {
				literal, ok := value.Values[0].(*ast.BasicLit)
				if ok {
					unquoted, err := strconv.Unquote(literal.Value)
					proxyIsolationArgDefined = err == nil && unquoted == "--no-proxy-server"
				}
			}
		case *ast.CallExpr:
			selector, ok := value.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "PutShouldDetectMonitorScaleChanges" || len(value.Args) != 1 {
				break
			}
			ident, ok := value.Args[0].(*ast.Ident)
			policyApplied = ok && ident.Name == "shouldDetectMonitorScaleChanges"
		case *ast.CompositeLit:
			ident, ok := value.Type.(*ast.Ident)
			if !ok || ident.Name != "Chromium" {
				break
			}
			for _, element := range value.Elts {
				entry, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := entry.Key.(*ast.Ident)
				if !ok || key.Name != "AdditionalBrowserArgs" {
					continue
				}
				args, ok := entry.Value.(*ast.CompositeLit)
				if !ok || len(args.Elts) != 1 {
					continue
				}
				arg, ok := args.Elts[0].(*ast.Ident)
				proxyIsolationArgApplied = ok && arg.Name == "reasonixNoProxyServerBrowserArg"
			}
		}
		return true
	})
	if !policyEnabled || !policyApplied {
		t.Fatal("patched WebView2 must enable and apply automatic monitor-scale detection")
	}
	if !proxyIsolationArgDefined || !proxyIsolationArgApplied {
		t.Fatal("patched WebView2 must pass --no-proxy-server to the browser process")
	}

	recoveryPolicyDefined := false
	recoveryPolicyApplied := false
	nativeReloadApplied := false
	for _, declaration := range parsed.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "shouldReloadFailedRenderer" {
			recoveryPolicyDefined = true
		}
		if fn.Name.Name != "ProcessFailed" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "shouldReloadFailedRenderer":
				recoveryPolicyApplied = true
			case "Reload":
				nativeReloadApplied = true
			}
			return true
		})
	}
	if !recoveryPolicyDefined || !recoveryPolicyApplied || !nativeReloadApplied {
		t.Fatal("patched WebView2 must throttle and natively reload failed main renderers")
	}

	coreFile := filepath.Join("third_party", "go-webview2", "pkg", "edge", "corewebview2.go")
	coreParsed, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	reloadWrapperDefined := false
	reloadVTableApplied := false
	for _, declaration := range coreParsed.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Reload" || fn.Recv == nil || fn.Body == nil {
			continue
		}
		reloadWrapperDefined = true
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "Reload" {
				reloadVTableApplied = true
			}
			return true
		})
	}
	if !reloadWrapperDefined || !reloadVTableApplied {
		t.Fatal("patched WebView2 must expose ICoreWebView2.Reload through the native COM vtable")
	}
}
