package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/taskcatalog"
)

func TestExtensionGenerationBumpsOnMCPMutationSites(t *testing.T) {
	// Guard mutation sites that publish shared Host / config changes while
	// controller builds may still be running off the lifecycle lock.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(".", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		sources = append(sources, string(raw))
	}
	src := strings.Join(sources, "\n")
	for _, name := range []string{
		"func (a *App) InstallMCPServer",
		"func (a *App) UpdateMCPServer",
		"func (a *App) RemoveMCPServer",
		"func (a *App) ReconnectMCPServer",
		"func (a *App) ClearMCPServerAuthentication",
	} {
		idx := strings.Index(src, name)
		if idx < 0 {
			t.Fatalf("missing %s", name)
		}
		rest := src[idx:]
		next := strings.Index(rest[len(name):], "\nfunc (a *App) ")
		body := rest
		if next >= 0 {
			body = rest[:len(name)+next]
		}
		if !strings.Contains(body, "a.bumpExtensionGeneration()") && !strings.Contains(body, "saveDesktopMCPServerAndBump") {
			t.Fatalf("%s does not bump extensionGeneration", name)
		}
	}
}

func TestControllerPublicationRejectsMutationThatCompletesAfterBoot(t *testing.T) {
	app := NewApp()
	generation := app.currentExtensionGeneration()

	// Model a build that has finished extension boot while an MCP mutation is
	// queued. The writer must finish and bump the generation before publication.
	app.extensionBuildMu.RLock()
	mutationLocked := make(chan struct{})
	releaseMutation := make(chan struct{})
	go func() {
		unlock := app.lockMCPMutation("test-publication-fence")
		close(mutationLocked)
		<-releaseMutation
		unlock()
	}()
	app.extensionBuildMu.RUnlock()
	<-mutationLocked

	publication := make(chan bool, 1)
	go func() {
		unlock, ok := app.lockTabControllerPublication(generation)
		if ok {
			unlock()
		}
		publication <- ok
	}()
	close(releaseMutation)
	if <-publication {
		t.Fatal("controller published after an MCP mutation changed its extension generation")
	}
}

func TestTaskActionProjectKeepsAllowlistedRoot(t *testing.T) {
	root := t.TempDir()
	app := &App{
		ctx: context.Background(),
		tabs: map[string]*WorkspaceTab{
			"active": {ID: "active", Scope: "project", WorkspaceRoot: root},
		},
		activeTabID: "active",
	}
	key := taskcatalog.ProjectKey(root)
	project, err := app.taskActionProject(key)
	if err != nil {
		t.Fatal(err)
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if project.Root != root {
		t.Fatalf("root = %q, want allowlisted %q", project.Root, root)
	}
}
