package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/remote/parity"
	"reasonix/internal/worktree"
)

func TestRemoteScopeManifestMethodsBeginWithGuard(t *testing.T) {
	methods := appMethodDeclarations(t)
	for _, entry := range parity.Entries() {
		helper := ""
		switch entry.Category {
		case parity.DeferredV1:
			helper = "rejectRemoteDeferred"
		case parity.OutOfScope:
			helper = "rejectRemoteOutOfScope"
		default:
			continue
		}

		decl := methods[entry.Method]
		if decl == nil {
			t.Errorf("%s has no *App method declaration", entry.Method)
			continue
		}
		if decl.Body == nil || len(decl.Body.List) == 0 {
			t.Errorf("%s has no method body", entry.Method)
			continue
		}
		if !statementCallsRemoteScopeGuard(decl.Body.List[0], helper, entry.Method) {
			t.Errorf("%s must call %s(%q) in its first statement", entry.Method, helper, entry.Method)
		}
	}
}

func appMethodDeclarations(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	methods := make(map[string]*ast.FuncDecl)
	files := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || !isAppReceiver(fn.Recv.List[0].Type) {
				continue
			}
			methods[fn.Name.Name] = fn
		}
	}
	return methods
}

func isAppReceiver(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "App"
}

func statementCallsRemoteScopeGuard(stmt ast.Stmt, helper, method string) bool {
	matched := false
	ast.Inspect(stmt, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != helper || len(call.Args) != 1 {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		literal, literalOK := call.Args[0].(*ast.BasicLit)
		if !ok || receiver.Name != "a" || !literalOK || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err == nil && value == method {
			matched = true
		}
		return true
	})
	return matched
}

func TestRemoteScopeGuardsRunBeforeLocalHostSideEffects(t *testing.T) {
	recording := newRemoteBridgeRecordingV1()
	app, _ := newRemoteBridgeV1TestApp(t, recording)

	oldInspect := inspectDeliveryWorktree
	oldCreate := createDeliveryWorktree
	t.Cleanup(func() {
		inspectDeliveryWorktree = oldInspect
		createDeliveryWorktree = oldCreate
	})
	inspectCalled := false
	createCalled := false
	inspectDeliveryWorktree = func(context.Context, string) worktree.Availability {
		inspectCalled = true
		return worktree.Availability{Available: true}
	}
	createDeliveryWorktree = func(context.Context, string, string) (worktree.Result, error) {
		createCalled = true
		return worktree.Result{}, nil
	}

	availability := app.DeliveryWorktreeAvailability("/must/not/be/inspected")
	if availability.Available || !strings.Contains(availability.Reason, ErrRemoteV1Deferred.Error()) {
		t.Fatalf("DeliveryWorktreeAvailability = %#v", availability)
	}
	if inspectCalled {
		t.Fatal("Remote DeliveryWorktreeAvailability inspected the local worktree")
	}
	if _, err := app.CreateDeliveryWorktree("/must/not/be/created"); !errors.Is(err, ErrRemoteV1Deferred) {
		t.Fatalf("CreateDeliveryWorktree error = %v", err)
	}
	if createCalled {
		t.Fatal("Remote CreateDeliveryWorktree created a local worktree")
	}

	deferredCalls := []struct {
		name string
		call func() error
	}{
		{"SavePastedFile", func() error {
			_, err := app.SavePastedFile("../../escape", "data:application/octet-stream;base64,QQ==")
			return err
		}},
		{"AttachmentDataURL", func() error {
			_, err := app.AttachmentDataURL("/etc/passwd")
			return err
		}},
		{"GitBranches", func() error {
			_, err := app.GitBranches()
			return err
		}},
		{"GitCheckout", func() error { return app.GitCheckout("--upload-pack=evil") }},
	}
	for _, tc := range deferredCalls {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrRemoteV1Deferred) {
				t.Fatalf("error = %v, want ErrRemoteV1Deferred", err)
			}
		})
	}

	outOfScopeCalls := []struct {
		name string
		call func() error
	}{
		{"OpenChannelSessionForTab", func() error {
			_, err := app.OpenChannelSessionForTab("missing", "/etc/passwd")
			return err
		}},
		{"AddSkillPath", func() error { return app.AddSkillPath("/must/not/be/stat-ed") }},
		{"AddMCPServer", func() error {
			_, err := app.AddMCPServer(MCPServerInput{Name: "forbidden", Command: "touch"})
			return err
		}},
		{"PlanPluginInstall", func() error {
			_, err := app.PlanPluginInstall("/must/not/be/read", PluginInstallOptions{})
			return err
		}},
		{"SetDefaultModel", func() error { return app.SetDefaultModel("provider/model") }},
		{"ConnectKey", func() error {
			_, err := app.ConnectKey("must-not-reach-network")
			return err
		}},
	}
	for _, tc := range outOfScopeCalls {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrRemoteOutOfScope) {
				t.Fatalf("error = %v, want ErrRemoteOutOfScope", err)
			}
		})
	}
}

func TestRemoteScopeHelpersPreserveLocalTarget(t *testing.T) {
	app := &App{}
	if err := app.rejectRemoteDeferred("local"); err != nil {
		t.Fatalf("rejectRemoteDeferred on Local target: %v", err)
	}
	if err := app.rejectRemoteOutOfScope("local"); err != nil {
		t.Fatalf("rejectRemoteOutOfScope on Local target: %v", err)
	}
}
