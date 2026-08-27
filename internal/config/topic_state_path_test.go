package config

import (
	"path/filepath"
	"testing"
)

func TestDesktopTopicStatePathUsesStateHome(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("REASONIX_STATE_HOME", stateHome)

	if got, want := DesktopTopicStatePath(""), filepath.Join(stateHome, "desktop", "topic-state-v1.sqlite"); got != want {
		t.Fatalf("global path = %q, want %q", got, want)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	abs, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(stateHome, "projects", WorkspaceSlug(abs), "desktop", "topic-state-v1.sqlite")
	if got := DesktopTopicStatePath(workspace); got != want {
		t.Fatalf("project path = %q, want %q", got, want)
	}
}
