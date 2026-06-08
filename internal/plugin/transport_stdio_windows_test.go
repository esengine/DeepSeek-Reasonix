//go:build windows

package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
