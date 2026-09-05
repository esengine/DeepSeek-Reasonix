package installlayout

import (
	"os"
	"path/filepath"
	"testing"
)

func seedVersionedLayout(t *testing.T, root, version, payload string) string {
	t.Helper()
	src := t.TempDir()
	members := make([]Member, 0, len(AllowedVersionMembers()))
	for _, name := range AllowedVersionMembers() {
		members = append(members, Member{
			Name: name,
			Path: writeTempMember(t, src, version+"-"+name, payload+"-"+name),
		})
	}
	launcherSrc := writeTempMember(t, src, version+"-launcher", payload+"-launcher")
	if err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     version,
		RequestID:   "relaunch-" + version,
		Members:     members,
		RootMembers: []Member{
			{Name: LauncherBinaryName(), Path: launcherSrc},
		},
		RequiredRootNames: []string{LauncherBinaryName()},
	}); err != nil {
		t.Fatal(err)
	}
	desktop, err := ActiveDesktopPath(root)
	if err != nil {
		t.Fatal(err)
	}
	return desktop
}

func TestStableRelaunchPathPrefersLauncherOverVersionedDesktop(t *testing.T) {
	root := t.TempDir()
	oldDesktop := seedVersionedLayout(t, root, "v1.24.0", "old")
	newDesktop := seedVersionedLayout(t, root, "v1.24.1", "new")
	if oldDesktop == newDesktop {
		t.Fatal("activation did not retain a distinct previous desktop")
	}
	if _, err := os.Stat(oldDesktop); err != nil {
		t.Fatalf("previous desktop should remain on disk: %v", err)
	}
	got, err := StableRelaunchPath(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, LauncherBinaryName())
	if got != want {
		t.Fatalf("StableRelaunchPath = %s, want launcher %s", got, want)
	}
}

func TestIsActiveDesktopExecutableRejectsRetainedPreviousVersion(t *testing.T) {
	root := t.TempDir()
	oldDesktop := seedVersionedLayout(t, root, "v1.24.0", "old")
	newDesktop := seedVersionedLayout(t, root, "v1.24.1", "new")

	active, err := IsActiveDesktopExecutable(newDesktop)
	if err != nil || !active {
		t.Fatalf("active desktop: active=%v err=%v", active, err)
	}
	active, err = IsActiveDesktopExecutable(oldDesktop)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("retained previous desktop must not count as the active executable")
	}
}

func TestIsSupersededVersionedDesktop(t *testing.T) {
	root := t.TempDir()
	oldDesktop := seedVersionedLayout(t, root, "v1.24.0", "old")
	newDesktop := seedVersionedLayout(t, root, "v1.24.1", "new")
	if !IsSupersededVersionedDesktop(root, oldDesktop) {
		t.Fatal("retained previous desktop should be superseded")
	}
	if IsSupersededVersionedDesktop(root, newDesktop) {
		t.Fatal("active desktop must not be superseded")
	}
	if IsSupersededVersionedDesktop(root, filepath.Join(root, LauncherBinaryName())) {
		t.Fatal("launcher is not a superseded desktop")
	}
	if IsSupersededVersionedDesktop(root, filepath.Join(t.TempDir(), DesktopBinaryName())) {
		t.Fatal("desktop outside the install root must not be treated as superseded")
	}
}

func TestIsActiveDesktopExecutableAllowsFlatInstall(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, DesktopBinaryName())
	if err := os.WriteFile(exe, []byte("flat"), 0o755); err != nil {
		t.Fatal(err)
	}
	active, err := IsActiveDesktopExecutable(exe)
	if err != nil || !active {
		t.Fatalf("flat desktop: active=%v err=%v", active, err)
	}
}

func TestStableRelaunchPathFallsBackToActiveDesktopWhenLauncherMissing(t *testing.T) {
	root := t.TempDir()
	desktop := seedVersionedLayout(t, root, "v1.24.1", "new")
	if err := os.Remove(filepath.Join(root, LauncherBinaryName())); err != nil {
		t.Fatal(err)
	}
	got, err := StableRelaunchPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != desktop {
		t.Fatalf("StableRelaunchPath = %s, want active desktop %s", got, desktop)
	}
}
