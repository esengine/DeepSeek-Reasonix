package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	versioned := false
	for _, directive := range mod.Require {
		if directive.Mod.Path == webview2ModulePath && directive.Mod.Version == "v1.0.28" {
			versioned = true
		}
	}
	for _, directive := range mod.Replace {
		if directive.Old.Path == webview2ModulePath && directive.New.Path == webview2PatchPath {
			replaced = true
			break
		}
	}
	if !replaced {
		t.Fatalf("%s must be replaced by %s", webview2ModulePath, webview2PatchPath)
	}
	if !versioned {
		t.Fatal("vendored WebView2 compatibility module must be pinned to upstream v1.0.28")
	}

	patchFile := filepath.Join("third_party", "go-webview2", "pkg", "edge", "chromium.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), patchFile, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	hostScaleWrite := false
	proxyIsolationArgDefined := false
	proxyIsolationArgApplied := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.ValueSpec:
			if len(value.Names) == 1 && value.Names[0].Name == "reasonixNoProxyServerBrowserArg" && len(value.Values) == 1 {
				literal, ok := value.Values[0].(*ast.BasicLit)
				if ok {
					unquoted, err := strconv.Unquote(literal.Value)
					proxyIsolationArgDefined = err == nil && unquoted == "--no-proxy-server"
				}
			}
		case *ast.CallExpr:
			selector, ok := value.Fun.(*ast.SelectorExpr)
			if !ok {
				break
			}
			if selector.Sel.Name == "PutShouldDetectMonitorScaleChanges" || selector.Sel.Name == "PutRasterizationScale" {
				hostScaleWrite = true
			}
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
	if hostScaleWrite {
		t.Fatal("WebView2 v1.0.28 must remain the sole rasterization-scale owner")
	}
	if !proxyIsolationArgDefined || !proxyIsolationArgApplied {
		t.Fatal("patched WebView2 must pass --no-proxy-server to the browser process")
	}

	recoveryPolicyDefined := false
	recoveryPolicyApplied := false
	recoveryCompletionApplied := false
	recoveryNavigationBound := false
	nativeReloadApplied := false
	nonFatalRecoveryErrors := false
	fatalRecoveryErrors := false
	diagnosticCollected := false
	diagnosticObserved := false
	for _, declaration := range parsed.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "beginFailedRendererRecovery" {
			recoveryPolicyDefined = true
		}
		if fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok {
				switch ident.Name {
				case "collectProcessFailedDiagnostic":
					diagnosticCollected = true
				case "notifyProcessFailedObserver":
					diagnosticObserved = true
				case "reload":
					if fn.Name.Name == "handleFailedRendererRecovery" {
						nativeReloadApplied = true
					}
				}
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "beginFailedRendererRecovery":
				if fn.Name.Name == "ProcessFailed" || fn.Name.Name == "handleFailedRendererRecovery" {
					recoveryPolicyApplied = true
				}
			case "completeFailedRendererRecovery":
				if fn.Name.Name == "NavigationCompleted" {
					recoveryCompletionApplied = true
				}
			case "Reload":
				if fn.Name.Name == "ProcessFailed" || fn.Name.Name == "handleFailedRendererRecovery" {
					nativeReloadApplied = true
				}
			case "bindNavigation":
				if fn.Name.Name == "NavigationStarting" {
					recoveryNavigationBound = true
				}
			case "nonFatalErrorCallback":
				if fn.Name.Name == "ProcessFailed" || fn.Name.Name == "handleFailedRendererRecovery" || fn.Name.Name == "completeFailedRendererRecovery" {
					nonFatalRecoveryErrors = true
				}
			case "errorCallback":
				if fn.Name.Name == "ProcessFailed" || fn.Name.Name == "completeFailedRendererRecovery" {
					fatalRecoveryErrors = true
				}
			}
			return true
		})
	}
	if !recoveryPolicyDefined || !recoveryPolicyApplied || !recoveryCompletionApplied || !recoveryNavigationBound || !nativeReloadApplied {
		t.Fatal("patched WebView2 must throttle and natively reload failed main renderers")
	}
	if !nonFatalRecoveryErrors || fatalRecoveryErrors {
		t.Fatal("renderer recovery failures must be reported without exiting the desktop")
	}
	if !diagnosticCollected || !diagnosticObserved {
		t.Fatal("patched WebView2 must collect and synchronously publish native process diagnostics")
	}

	windowSyncOrder := []string{"GetClientRect", "ResizeWithBounds", "NotifyParentWindowPositionChanged"}
	windowSyncPosition := -1
	for _, declaration := range parsed.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "syncWindowState" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || windowSyncPosition+1 >= len(windowSyncOrder) || selector.Sel.Name != windowSyncOrder[windowSyncPosition+1] {
				return true
			}
			windowSyncPosition++
			return true
		})
	}
	if windowSyncPosition != len(windowSyncOrder)-1 {
		t.Fatal("window synchronization must read client rect, set bounds, then notify parent position")
	}

	argsFile := filepath.Join("third_party", "go-webview2", "pkg", "edge", "ICoreWebView2ProcessFailedEventArgs.go")
	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"{4DAB9422-46FA-4C3E-A5D2-41D2071D3680}",
		"{AB667428-094D-5FD1-B480-8B4C0FDBDF2F}",
		"GetICoreWebView2ProcessFailedEventArgs2",
		"GetICoreWebView2ProcessFailedEventArgs3",
	} {
		if !strings.Contains(string(argsData), expected) {
			t.Fatalf("patched WebView2 process-failed args must include %s", expected)
		}
	}
	for _, requiredFile := range []string{
		"ICoreWebView2NavigationStartingEventArgs.go",
		"ICoreWebView2NavigationStartingEventHandler.go",
		"ICoreWebView2ProcessFailedEventArgs2.go",
		"ICoreWebView2ProcessFailedEventArgs3.go",
		"process_failed_diagnostics.go",
	} {
		if _, err := os.Stat(filepath.Join("third_party", "go-webview2", "pkg", "edge", requiredFile)); err != nil {
			t.Fatalf("required native diagnostics patch %s is missing: %v", requiredFile, err)
		}
	}
	diagnosticsData, err := os.ReadFile(filepath.Join("third_party", "go-webview2", "pkg", "edge", "process_failed_diagnostics.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diagnosticsData), "COREWEBVIEW2_PROCESS_FAILED_KIND_UNKNOWN_PROCESS_EXITED") {
		t.Fatal("a failed process-kind getter must not default to a fatal browser exit")
	}
	navigationArgsData, err := os.ReadFile(filepath.Join("third_party", "go-webview2", "pkg", "edge", "ICoreWebView2NavigationCompletedEventArgs.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"GetIsSuccess", "GetNavigationID"} {
		if !strings.Contains(string(navigationArgsData), expected) {
			t.Fatalf("renderer recovery navigation completion must include %s", expected)
		}
	}

	installerData, err := os.ReadFile("webview2_diagnostics_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installerData), "edge.SetProcessFailedObserver") {
		t.Fatal("Windows desktop must install the vendored WebView2 process-failed observer")
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
