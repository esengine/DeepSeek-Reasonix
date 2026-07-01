package main

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

const desktopTinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestWorkspaceRelativeIn(t *testing.T) {
	root := t.TempDir()

	if rel, ok := workspaceRelativeIn(filepath.Join(root, "sub", "file.go"), root); !ok || rel != "sub/file.go" {
		t.Fatalf("in-tree = (%q, %v), want (sub/file.go, true)", rel, ok)
	}
	if _, ok := workspaceRelativeIn(filepath.Join(filepath.Dir(root), "sibling.txt"), root); ok {
		t.Fatal("a path above the workspace must not resolve as in-tree")
	}
}

func TestIsImageExt(t *testing.T) {
	for _, p := range []string{"a.png", "A.PNG", "b.jpeg", "c.webp"} {
		if !isImageExt(p) {
			t.Errorf("%q should be an image extension", p)
		}
	}
	for _, p := range []string{"notes.pdf", "main.go", "noext"} {
		if isImageExt(p) {
			t.Errorf("%q should not be an image extension", p)
		}
	}
}

func TestSavePastedImageUsesActiveWorkspaceRoot(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	launchRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	projectRoot, _ = os.Getwd()
	if err := os.Chdir(launchRoot); err != nil {
		t.Fatal(err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", WorkspaceRoot: projectRoot},
		},
		activeTabID: "project",
	}

	got, err := app.SavePastedImage("data:image/png;base64," + desktopTinyPNG)
	if err != nil {
		t.Fatalf("SavePastedImage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(got))); err != nil {
		t.Fatalf("pasted image should be saved under active workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(launchRoot, filepath.FromSlash(got))); !os.IsNotExist(err) {
		t.Fatalf("pasted image should not be saved under launch root, stat err=%v", err)
	}
	preview, err := app.AttachmentDataURL(got)
	if err != nil {
		t.Fatalf("AttachmentDataURL: %v", err)
	}
	if !strings.HasPrefix(preview, "data:image/png;base64,") {
		t.Fatalf("preview = %q, want png data URL", preview)
	}
}

func TestSavePastedImageUsesPinnedSessionOwnerBeforeStaleWorkspaceRoot(t *testing.T) {
	isolateDesktopUserDirs(t)
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	launchRoot := t.TempDir()
	projectA := t.TempDir()
	projectB := t.TempDir()
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}
	sessionDirA := desktopSessionDir(projectA)
	if err := os.MkdirAll(sessionDirA, 0o755); err != nil {
		t.Fatalf("mkdir project A sessions: %v", err)
	}
	sessionPathA := writeTopicSessionWithPrompt(t, sessionDirA, "project-a.jsonl", "topic_attach_owner", "Attach owner", projectA, "project A prompt", time.Now())
	if err := os.Chdir(launchRoot); err != nil {
		t.Fatal(err)
	}

	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", Scope: "project", WorkspaceRoot: projectB, SessionPath: sessionPathA},
		},
		activeTabID: "project",
	}

	got, err := app.SavePastedImage("data:image/png;base64," + desktopTinyPNG)
	if err != nil {
		t.Fatalf("SavePastedImage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectA, filepath.FromSlash(got))); err != nil {
		t.Fatalf("pasted image should be saved under pinned session owner project A: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectB, filepath.FromSlash(got))); !os.IsNotExist(err) {
		t.Fatalf("pasted image should not be saved under stale project B, stat err=%v", err)
	}
	if gotRoot := normalizeProjectRoot(app.tabs["project"].WorkspaceRoot); gotRoot != normalizeProjectRoot(projectA) {
		t.Fatalf("tab workspace root = %q, want project A %q", gotRoot, normalizeProjectRoot(projectA))
	}
}

func TestAttachDroppedUsesActiveWorkspaceRoot(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	launchRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatal(err)
	}
	projectRoot, _ = os.Getwd()
	if err := os.Chdir(launchRoot); err != nil {
		t.Fatal(err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", WorkspaceRoot: projectRoot},
		},
		activeTabID: "project",
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectRoot, "sub", "notes.txt")
	if err := os.WriteFile(target, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := app.AttachDropped(target)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "workspace" || got.Path != "sub/notes.txt" {
		t.Fatalf("got %+v, want workspace ref sub/notes.txt", got)
	}
}

func TestAttachDroppedImageUsesActiveWorkspaceRoot(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	launchRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.Chdir(launchRoot); err != nil {
		t.Fatal(err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", WorkspaceRoot: projectRoot},
		},
		activeTabID: "project",
	}
	raw, err := base64.StdEncoding.DecodeString(desktopTinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(outside, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := app.AttachDropped(outside)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "attachment" || !strings.HasSuffix(got.Path, ".png") {
		t.Fatalf("got %+v, want png attachment", got)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(got.Path))); err != nil {
		t.Fatalf("dropped image should be saved under active workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(launchRoot, filepath.FromSlash(got.Path))); !os.IsNotExist(err) {
		t.Fatalf("dropped image should not be saved under launch root, stat err=%v", err)
	}
	if !strings.HasPrefix(got.PreviewURL, "data:image/png;base64,") {
		t.Fatalf("preview = %q, want png data URL", got.PreviewURL)
	}
}

func TestAttachDroppedInWorkspaceReferencesInPlace(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.MkdirAll(filepath.Join(cwd, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cwd, "sub", "notes.txt")
	if err := os.WriteFile(target, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := (&App{}).AttachDropped(target)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "workspace" || got.Path != "sub/notes.txt" {
		t.Fatalf("got %+v, want workspace ref sub/notes.txt", got)
	}
}

func TestAttachDroppedOutsideWorkspaceCopiesToAttachments(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	outside := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(outside, []byte("%PDF body"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	got, err := (&App{}).AttachDropped(outside)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "attachment" || !strings.HasPrefix(got.Path, ".reasonix/attachments/") || !strings.HasSuffix(got.Path, ".pdf") {
		t.Fatalf("got %+v, want copied pdf attachment", got)
	}
}

func TestAttachDroppedImageStoresThumbnail(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	if err := os.WriteFile(filepath.Join(cwd, "shot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := (&App{}).AttachDropped(filepath.Join(cwd, "shot.png"))
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "attachment" || !strings.HasSuffix(got.Path, ".png") {
		t.Fatalf("got %+v, want png attachment", got)
	}
	if !strings.HasPrefix(got.PreviewURL, "data:image/png;base64,") {
		t.Fatalf("preview = %q, want png data URL", got.PreviewURL)
	}
}

func TestAttachDroppedOutsideWorkspaceDirRegistersWorkspaceRef(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "Folder With Spaces")
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "sub", "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectedOutside := outside
	if resolved, err := filepath.EvalSymlinks(outside); err == nil {
		expectedOutside = resolved
	}
	expectedDisplayPath := filepath.ToSlash(expectedOutside)
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}

	ctrl := control.New(control.Options{WorkspaceRoot: workspace})
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", WorkspaceRoot: workspace, Ctrl: ctrl},
		},
		activeTabID: "project",
	}

	got, err := app.AttachDropped(outside)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if got.Kind != "workspace" || !got.IsDir {
		t.Fatalf("got %+v, want workspace directory ref", got)
	}
	if !strings.HasPrefix(got.Path, "__reasonix_external_folder/") || strings.ContainsAny(got.Path, " \t\r\n") {
		t.Fatalf("external folder path token = %q, want whitespace-free external token", got.Path)
	}
	if got.DisplayPath != expectedDisplayPath {
		t.Fatalf("display path = %q, want %q", got.DisplayPath, expectedDisplayPath)
	}

	block, errs := ctrl.ResolveScopedRefs(context.Background(), "inspect @"+got.Path+"/")
	if len(errs) != 0 {
		t.Fatalf("ResolveScopedRefs errors = %v", errs)
	}
	if !strings.Contains(block, `<dir path="`+expectedDisplayPath+`">`) ||
		!strings.Contains(block, "sub/") ||
		!strings.Contains(block, "sub/notes.txt") {
		t.Fatalf("external dropped folder should resolve as dir context:\n%s", block)
	}
}

func TestAttachDroppedOutsideWorkspaceDirRegistersAfterPinnedOwnerRebuild(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "TEST_MODEL_KEY", "sk-test")
	cfg := config.Default()
	cfg.DefaultModel = "test/test-model"
	cfg.Desktop.ProviderAccess = []string{"test"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "test", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "test-model", APIKeyEnv: "TEST_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	projectA := t.TempDir()
	projectB := t.TempDir()
	outside := filepath.Join(t.TempDir(), "External")
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "sub", "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}
	sessionDirA := desktopSessionDir(projectA)
	sessionDirB := desktopSessionDir(projectB)
	if err := os.MkdirAll(sessionDirA, 0o755); err != nil {
		t.Fatalf("mkdir project A sessions: %v", err)
	}
	if err := os.MkdirAll(sessionDirB, 0o755); err != nil {
		t.Fatalf("mkdir project B sessions: %v", err)
	}
	sessionPathA := writeTopicSessionWithPrompt(t, sessionDirA, "project-a.jsonl", "topic_external_ref", "External ref", projectA, "project A prompt", time.Now())
	sessionPathB := filepath.Join(sessionDirB, "wrong.jsonl")
	oldCtrl := control.New(control.Options{
		SessionDir:    sessionDirB,
		SessionPath:   sessionPathB,
		WorkspaceRoot: projectB,
		Sink:          event.Discard,
	})
	app := NewApp()
	app.readyHook = func() {}
	tab := &WorkspaceTab{
		ID:            "project",
		Scope:         "project",
		WorkspaceRoot: projectB,
		TopicID:       "topic_external_ref",
		TopicTitle:    "External ref",
		SessionPath:   sessionPathA,
		Ready:         true,
		model:         "test/test-model",
		Ctrl:          oldCtrl,
		sink:          &tabEventSink{tabID: "project", app: app},
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
	})

	got, err := app.AttachDropped(outside)
	if err != nil {
		t.Fatalf("AttachDropped: %v", err)
	}
	if tab.Ctrl == oldCtrl {
		t.Fatal("stale controller was reused for external folder ref")
	}
	if gotRoot := normalizeProjectRoot(tab.Ctrl.WorkspaceRoot()); gotRoot != normalizeProjectRoot(projectA) {
		t.Fatalf("controller workspace root = %q, want project A %q", gotRoot, normalizeProjectRoot(projectA))
	}
	resolver, ok := tab.Ctrl.(interface {
		ResolveScopedRefs(context.Context, string) (string, []string)
	})
	if !ok {
		t.Fatalf("rebuilt controller does not resolve scoped refs: %T", tab.Ctrl)
	}
	block, errs := resolver.ResolveScopedRefs(context.Background(), "inspect @"+got.Path+"/")
	if len(errs) != 0 {
		t.Fatalf("ResolveScopedRefs errors = %v", errs)
	}
	if !strings.Contains(block, "sub/notes.txt") {
		t.Fatalf("external dropped folder should resolve on rebuilt controller:\n%s", block)
	}
}
