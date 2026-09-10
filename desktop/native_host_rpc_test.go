package main

import (
	"context"
	"testing"
)

func TestRPCNativeHostMapsDialogsAndQueries(t *testing.T) {
	a, _, shell := newHostShellBridgeForTest(t, map[string]any{
		"host/dialog.openDirectory": map[string]any{"path": "/pick/dir"},
		"host/dialog.openFile":      map[string]any{"paths": []string{"/pick/a.png", "/pick/b.png"}},
		"host/dialog.saveFile":      map[string]any{"path": "/save/out.json"},
		"host/dialog.message":       map[string]any{"button": "Quit"},
		"host/window.isMaximised":   map[string]any{"value": true},
		"host/screen.list": map[string]any{"screens": []map[string]any{
			{"x": 0, "y": 0, "width": 2560, "height": 1440, "scale": 2.0, "primary": true},
			{"x": 2560, "y": 0, "width": 1920, "height": 1080, "scale": 1.0, "primary": false},
		}},
	})
	for _, method := range []string{"host/dialog.openDirectory", "host/dialog.openFile", "host/dialog.saveFile", "host/dialog.message", "host/window.isMaximised", "host/screen.list", "host/window.setPosition", "host/shell.openExternal"} {
		shell.handle(method)
	}
	host := a.nativeHost()
	ctx := context.Background()
	if dir, err := host.OpenDirectoryDialog(ctx, nativeDialogOptions{Title: "Pick"}); err != nil || dir != "/pick/dir" {
		t.Fatalf("openDirectory = %q, %v", dir, err)
	}
	if file, err := host.OpenFileDialog(ctx, nativeDialogOptions{Filters: []nativeFileFilter{{DisplayName: "Images", Pattern: "*.png"}}}); err != nil || file != "/pick/a.png" {
		t.Fatalf("openFile = %q, %v", file, err)
	}
	if path, err := host.SaveFileDialog(ctx, nativeDialogOptions{DefaultFilename: "out.json"}); err != nil || path != "/save/out.json" {
		t.Fatalf("saveFile = %q, %v", path, err)
	}
	if button, err := host.MessageDialog(ctx, nativeMessageOptions{Type: nativeDialogQuestion, Buttons: []string{"Quit", "Cancel"}}); err != nil || button != "Quit" {
		t.Fatalf("message = %q, %v", button, err)
	}
	if !host.WindowIsMaximised(ctx) {
		t.Fatal("isMaximised must follow the shell reply")
	}
	screens, err := host.Screens(ctx)
	if err != nil || len(screens) != 2 || !screens[0].Primary || screens[0].Width != 2560 || screens[1].Scale != 1 {
		t.Fatalf("screens = %+v, %v", screens, err)
	}
	host.SetWindowPosition(ctx, 10, 20)
	host.OpenExternal(ctx, "https://example.test")
	calls := shell.methods()
	if len(calls) != 8 || calls[6] != "host/window.setPosition" || calls[7] != "host/shell.openExternal" {
		t.Fatalf("shell calls = %v", calls)
	}
}

func TestRPCNativeHostToleratesAnUnansweredShell(t *testing.T) {
	a, _, _ := newHostShellBridgeForTest(t, nil)
	host := a.nativeHost()
	ctx := context.Background()
	if dir, err := host.OpenDirectoryDialog(ctx, nativeDialogOptions{}); err == nil || dir != "" {
		t.Fatalf("an unhandled dialog must fail, got %q, %v", dir, err)
	}
	if host.WindowIsMaximised(ctx) {
		t.Fatal("an unhandled query must read as false")
	}
	if _, err := host.Screens(ctx); err == nil {
		t.Fatal("an unhandled screen list must surface an error")
	}
}
