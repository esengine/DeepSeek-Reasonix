package plugin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/mcplaunch"
)

// TestNewStdioTransportDirExplicit verifies that explicit Spec.Dir takes
// precedence over WorkspaceRoot for cmd.Dir.
func TestNewStdioTransportDirExplicit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	explicitDir := filepath.Join(t.TempDir(), "explicit")
	if err := os.MkdirAll(explicitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Name:          "test-dir",
		Command:       exe,
		Args:          []string{"-test.run=TestHelperProcess", "--"},
		Dir:           explicitDir,
		WorkspaceRoot: workspaceRoot,
		Env:           map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}
	tr, err := newStdioTransport(context.Background(), spec)
	if err != nil {
		t.Fatalf("newStdioTransport: %v", err)
	}
	defer tr.close()
	if tr.cmd.Dir != explicitDir {
		t.Fatalf("cmd.Dir = %q, want %q (explicit Dir should take precedence)", tr.cmd.Dir, explicitDir)
	}
}

// TestNewStdioTransportProjectDirFallbackWorkspaceRoot verifies that when
// Spec.Dir is empty, a project-provided subprocess falls back to WorkspaceRoot.
// This prevents relative config file paths (e.g. --config-file ssh-config.json
// in .mcp.json) from resolving against the desktop process CWD instead of the
// project root where the config file lives (#6778).
func TestNewStdioTransportProjectDirFallbackWorkspaceRoot(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Name:                  "test-fallback",
		Command:               exe,
		Args:                  []string{"-test.run=TestHelperProcess", "--"},
		WorkspaceRoot:         workspaceRoot,
		RequireLaunchApproval: true,
		Env:                   map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}
	tr, err := newStdioTransport(context.Background(), spec)
	if err != nil {
		t.Fatalf("newStdioTransport: %v", err)
	}
	defer tr.close()
	if tr.cmd.Dir != workspaceRoot {
		t.Fatalf("cmd.Dir = %q, want %q (should fall back to WorkspaceRoot when Dir is empty)", tr.cmd.Dir, workspaceRoot)
	}
}

// TestNewStdioTransportUserScopesKeepInheritedDir preserves WorkspaceRoot as
// roots/list metadata for installed servers without changing their process CWD.
func TestNewStdioTransportUserScopesKeepInheritedDir(t *testing.T) {
	for _, source := range []string{"user_config", "legacy_user_config", "plugin_package"} {
		t.Run(source, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			spec := Spec{
				Name:          "test-user-scope",
				Command:       exe,
				Args:          []string{"-test.run=TestHelperProcess", "--"},
				WorkspaceRoot: t.TempDir(),
				ConfigSource:  source,
				Env:           map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
			}
			tr, err := newStdioTransport(context.Background(), spec)
			if err != nil {
				t.Fatalf("newStdioTransport: %v", err)
			}
			defer tr.close()
			if tr.cmd.Dir != "" {
				t.Fatalf("cmd.Dir = %q, want inherited process CWD", tr.cmd.Dir)
			}
		})
	}
}

func TestProjectRelativeExecutableResolutionMatchesLaunchIdentity(t *testing.T) {
	processRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Chdir(processRoot)

	name := "server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	relativeCommand := "." + string(os.PathSeparator) + name
	processExecutable := filepath.Join(processRoot, name)
	workspaceExecutable := filepath.Join(workspaceRoot, name)
	if err := os.WriteFile(processExecutable, []byte("process cwd executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workspaceExecutable, []byte("workspace executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	spec := Spec{
		Name:                  "project-relative-command",
		Command:               relativeCommand,
		WorkspaceRoot:         workspaceRoot,
		RequireLaunchApproval: true,
	}
	exe, _, err := resolveStdioExecutable(context.Background(), spec, os.Environ())
	if err != nil {
		t.Fatalf("resolveStdioExecutable: %v", err)
	}
	identity, err := buildProjectLaunchIdentity(context.Background(), spec)
	if err != nil {
		t.Fatalf("buildProjectLaunchIdentity: %v", err)
	}
	if exe != workspaceExecutable || identity.CommandPath != workspaceExecutable {
		t.Fatalf("resolved executable = %q, identity path = %q, want %q", exe, identity.CommandPath, workspaceExecutable)
	}
	if identity.Dir != workspaceRoot {
		t.Fatalf("identity.Dir = %q, want %q", identity.Dir, workspaceRoot)
	}
	wantWorkspaceHash, err := mcplaunch.FileSHA256(workspaceExecutable)
	if err != nil {
		t.Fatal(err)
	}
	processHash, err := mcplaunch.FileSHA256(processExecutable)
	if err != nil {
		t.Fatal(err)
	}
	if identity.CommandSHA256 != wantWorkspaceHash || identity.CommandSHA256 == processHash {
		t.Fatalf("identity executable hash = %q, want workspace hash %q and not process hash %q", identity.CommandSHA256, wantWorkspaceHash, processHash)
	}

	before, err := mcplaunch.ProjectLaunchIdentityDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workspaceExecutable, []byte("changed workspace executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	changedIdentity, err := buildProjectLaunchIdentity(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	after, err := mcplaunch.ProjectLaunchIdentityDigest(changedIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("workspace executable mutation did not invalidate the launch identity")
	}
}

// TestNewStdioTransportDirEmptyWhenBothEmpty verifies that cmd.Dir remains
// empty (inherits parent CWD) when both Dir and WorkspaceRoot are empty.
func TestNewStdioTransportDirEmptyWhenBothEmpty(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Name:    "test-empty",
		Command: exe,
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}
	tr, err := newStdioTransport(context.Background(), spec)
	if err != nil {
		t.Fatalf("newStdioTransport: %v", err)
	}
	defer tr.close()
	if tr.cmd.Dir != "" {
		t.Fatalf("cmd.Dir = %q, want empty (should inherit parent CWD when both Dir and WorkspaceRoot are empty)", tr.cmd.Dir)
	}
}

// TestNewStdioTransportDirDoesNotOverwriteForCodeGraph confirms the fix does
// not regress CodeGraph / codebase-memory-mcp which set Dir via
// ApplyKnownOverrides.
func TestNewStdioTransportDirDoesNotOverwriteForCodeGraph(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate what ApplyKnownOverrides does: set Dir = workspaceRoot for CodeGraph.
	spec := Spec{
		Name:          "codegraph",
		Command:       exe,
		Args:          []string{"-test.run=TestHelperProcess", "--"},
		Dir:           projectRoot, // set by ApplyKnownOverrides
		WorkspaceRoot: projectRoot,
		Env:           map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
		LowPriority:   true,
	}
	tr, err := newStdioTransport(context.Background(), spec)
	if err != nil {
		t.Fatalf("newStdioTransport: %v", err)
	}
	defer tr.close()
	if tr.cmd.Dir != projectRoot {
		t.Fatalf("cmd.Dir = %q, want %q (CodeGraph Dir should be preserved)", tr.cmd.Dir, projectRoot)
	}
}
