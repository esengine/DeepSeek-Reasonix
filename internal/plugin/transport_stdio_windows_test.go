//go:build windows

package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResolveStdioExecutableWindowsPathAndPATHEXT(t *testing.T) {
	dir := t.TempDir()
	npx := filepath.Join(dir, "npx.cmd")
	if err := os.WriteFile(npx, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatalf("write fake npx.cmd: %v", err)
	}

	exe, env, err := resolveStdioExecutable(context.Background(), Spec{Name: "fs", Command: "npx"}, []string{
		"Path=" + dir,
		"PATHEXT=.CMD;.EXE",
	})
	if err != nil {
		t.Fatalf("resolveStdioExecutable: %v", err)
	}
	if !strings.EqualFold(exe, npx) {
		t.Fatalf("resolved executable = %q, want %q", exe, npx)
	}
	if got, ok := envValue(env, "PATH"); !ok || got != dir {
		t.Fatalf("env PATH = %q, %v; want %q, true", got, ok, dir)
	}
}

func TestResolveStdioExecutableWindowsUsesCommonNodeFallback(t *testing.T) {
	root := t.TempDir()
	localAppData := filepath.Join(root, "Local")
	nodeDir := filepath.Join(localAppData, "Programs", "nodejs")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npx := filepath.Join(nodeDir, "npx.cmd")
	if err := os.WriteFile(npx, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatalf("write fake npx.cmd: %v", err)
	}

	exe, env, err := resolveStdioExecutable(context.Background(), Spec{Name: "fs", Command: "npx"}, []string{
		"Path=",
		"PATHEXT=.CMD;.EXE",
		"LOCALAPPDATA=" + localAppData,
	})
	if err != nil {
		t.Fatalf("resolveStdioExecutable: %v", err)
	}
	if !strings.EqualFold(exe, npx) {
		t.Fatalf("resolved executable = %q, want %q", exe, npx)
	}
	if got, ok := envValue(env, "PATH"); !ok || !strings.Contains(strings.ToLower(got), strings.ToLower(nodeDir)) {
		t.Fatalf("env PATH = %q, %v; want node fallback dir", got, ok)
	}
}

func TestSetEnvValueWindowsReplacesPathCaseInsensitively(t *testing.T) {
	env := setEnvValue([]string{"Path=C:\\old", "OTHER=x"}, "PATH", "C:\\new")
	if got, ok := envValue(env, "Path"); !ok || got != "C:\\new" {
		t.Fatalf("env Path = %q, %v; want C:\\new, true", got, ok)
	}
	if len(env) != 2 {
		t.Fatalf("setEnvValue should replace Path instead of appending PATH, got %v", env)
	}
}

func TestStdioTransportRequiresWindowsJobOwnership(t *testing.T) {
	t.Setenv("GO_WANT_STDIO_TRACKING_HELPER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	transport, err := newStdioTransport(ctx, Spec{
		Name:    "tracking-contract",
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestStdioTrackingHelper$"},
	})
	if err != nil {
		t.Fatalf("start stdio transport: %v", err)
	}
	t.Cleanup(transport.close)
	if transport.job == 0 {
		t.Fatal("Windows stdio transport started without required Job Object ownership")
	}

	var closes sync.WaitGroup
	for range 8 {
		closes.Add(1)
		go func() {
			defer closes.Done()
			transport.close()
		}()
	}
	closes.Wait()
	if state := transport.cmd.ProcessState; state == nil || !state.Exited() {
		t.Fatalf("tracked stdio helper was not reaped: %#v", state)
	}
}

func TestStdioTrackingHelper(t *testing.T) {
	if os.Getenv("GO_WANT_STDIO_TRACKING_HELPER") != "1" {
		return
	}
	time.Sleep(10 * time.Minute)
}
