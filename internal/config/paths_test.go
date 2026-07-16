package config

import (
	"runtime"
	"testing"
)

func TestProjectRootFromWorktree(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string // expected result (relative form for readability)
		changed bool   // whether we expect the result to differ from input
	}{
		{
			name:    "inside worktree subdir",
			path:    "/project/.worktrees/feat-x/cmd",
			want:    "/project",
			changed: true,
		},
		{
			name:    "worktree root itself",
			path:    "/project/.worktrees/feat-x",
			want:    "/project",
			changed: true,
		},
		{
			name:    ".worktrees dir itself (edge case)",
			path:    "/project/.worktrees",
			want:    "/project",
			changed: true,
		},
		{
			name:    "not under worktrees",
			path:    "/project/src",
			want:    "/project/src",
			changed: false,
		},
		{
			name:    "project root itself",
			path:    "/project",
			want:    "/project",
			changed: false,
		},
		{
			name:    "unrelated path",
			path:    "/home/user/other-project",
			want:    "/home/user/other-project",
			changed: false,
		},
		{
			name:    "empty worktrees name",
			path:    "/project/.worktrees//src",
			want:    "/project",
			changed: true,
		},
		{
			name:    "deep nesting inside worktree",
			path:    "/project/.worktrees/hotfix/internal/pkg/deep",
			want:    "/project",
			changed: true,
		},
		{
			name:    "non-absolute path unchanged",
			path:    ".worktrees/feat-x",
			want:    ".worktrees/feat-x",
			changed: false,
		},
		{
			name:    "relative path with worktrees segment",
			path:    "project/.worktrees/feat-x",
			want:    "project/.worktrees/feat-x",
			changed: false,
		},
		{
			name:    "worktrees-like but not exact match",
			path:    "/project/my.worktrees/feat-x",
			want:    "/project/my.worktrees/feat-x",
			changed: false,
		},
		{
			name:    "worktrees as part of deeper name",
			path:    "/project/x.worktrees.y/feat-x",
			want:    "/project/x.worktrees.y/feat-x",
			changed: false,
		},
		{
			name:    "nested duplicate worktree path",
			path:    "/project/.worktrees/pr-worktree/.worktrees/pr-worktree",
			want:    "/project",
			changed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProjectRootFromWorktree(tt.path)
			if got != tt.want {
				t.Errorf("ProjectRootFromWorktree(%q) = %q, want %q", tt.path, got, tt.want)
			}
			if !tt.changed && got != tt.path {
				t.Errorf("ProjectRootFromWorktree(%q) = %q, expected unchanged (%q)", tt.path, got, tt.path)
			}
		})
	}
}

func TestProjectRootFromWorktree_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only path test")
	}

	got := ProjectRootFromWorktree(`C:\project\.worktrees\feat-x\cmd`)
	want := `C:\project`
	if got != want {
		t.Errorf("ProjectRootFromWorktree(…) = %q, want %q", got, want)
	}

	// Unchanged when no .worktrees segment
	got = ProjectRootFromWorktree(`C:\project\src`)
	want = `C:\project\src`
	if got != want {
		t.Errorf("ProjectRootFromWorktree(…) = %q, want %q", got, want)
	}
}

func TestProjectSessionDir_WorktreeRouting(t *testing.T) {
	// This test verifies that ProjectSessionDir and MemoryCompilerDir
	// route worktree paths to the same project slug as the parent root.
	parent := "/home/user/my-project"
	worktree := "/home/user/my-project/.worktrees/feat-x"

	parentDir := ProjectSessionDir(parent)
	worktreeDir := ProjectSessionDir(worktree)

	if parentDir == "" {
		t.Fatal("ProjectSessionDir(parent) returned empty")
	}
	if parentDir != worktreeDir {
		t.Errorf("ProjectSessionDir mismatch:\n  parent:  %q\n  worktree: %q", parentDir, worktreeDir)
	}

	parentMem := MemoryCompilerDir(parent)
	worktreeMem := MemoryCompilerDir(worktree)

	if parentMem == "" {
		t.Fatal("MemoryCompilerDir(parent) returned empty")
	}
	if parentMem != worktreeMem {
		t.Errorf("MemoryCompilerDir mismatch:\n  parent:  %q\n  worktree: %q", parentMem, worktreeMem)
	}

	// Verify slug contains the parent project name, not the worktree name.
	slug := WorkspaceSlug(ProjectRootFromWorktree(worktree))
	expectedSlug := WorkspaceSlug(parent)
	if slug != expectedSlug {
		t.Errorf("slug from worktree = %q, expected %q", slug, expectedSlug)
	}
}

func TestProjectSessionDir_UnchangedWithoutWorktree(t *testing.T) {
	// Paths without .worktrees/ segment should resolve independently.
	dirA := ProjectSessionDir("/home/user/project-a")
	dirB := ProjectSessionDir("/home/user/project-b")

	if dirA == "" || dirB == "" {
		t.Fatal("ProjectSessionDir returned empty for normal paths")
	}
	if dirA == dirB {
		t.Error("two different projects should have different session dirs")
	}

	// A non-worktree subpath should NOT collapse to the parent.
	dirSrc := ProjectSessionDir("/home/user/project-a/src")
	if dirSrc == dirA {
		t.Error("/project-a/src should not collapse to /project-a without .worktrees/")
	}
}

func TestWorkspaceSlug(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/project", "-home-user-project"},
		{"/a/b/c", "-a-b-c"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := WorkspaceSlug(tt.path); got != tt.want {
				t.Errorf("WorkspaceSlug(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
