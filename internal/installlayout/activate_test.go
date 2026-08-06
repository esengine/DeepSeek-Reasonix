package installlayout

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeTempMember(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestActivateVersionAtomicPointerSwap(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	version := "v1.20.0"
	members := make([]Member, 0, 3)
	for _, name := range AllowedVersionMembers() {
		members = append(members, Member{
			Name: name,
			Path: writeTempMember(t, src, name, "payload-"+name),
		})
	}
	if err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     version,
		RequestID:   "req-activate-1",
		Members:     members,
	}); err != nil {
		t.Fatal(err)
	}
	ptr, err := ReadCurrent(root)
	if err != nil {
		t.Fatal(err)
	}
	if ptr.ActiveVersion != version {
		t.Fatalf("active=%s", ptr.ActiveVersion)
	}
	desktop, err := ActiveDesktopPath(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(desktop)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "payload-"+DesktopBinaryName() {
		t.Fatalf("desktop payload = %q", raw)
	}
}

func TestActivateVersionKeepsOldPointerOnMissingMember(t *testing.T) {
	root := t.TempDir()
	// Seed an existing active version.
	oldSrc := t.TempDir()
	oldMembers := make([]Member, 0, 3)
	for _, name := range AllowedVersionMembers() {
		oldMembers = append(oldMembers, Member{Name: name, Path: writeTempMember(t, oldSrc, name, "old")})
	}
	if err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     "v1.19.1",
		RequestID:   "seed",
		Members:     oldMembers,
	}); err != nil {
		t.Fatal(err)
	}

	src := t.TempDir()
	// Omit update helper — activation must fail and leave v1.19.1 active.
	bad := []Member{
		{Name: DesktopBinaryName(), Path: writeTempMember(t, src, DesktopBinaryName(), "new")},
		{Name: CLIBinaryName(), Path: writeTempMember(t, src, CLIBinaryName(), "new")},
	}
	if err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     "v1.20.0",
		RequestID:   "bad",
		Members:     bad,
	}); err == nil {
		t.Fatal("expected missing member failure")
	}
	ptr, err := ReadCurrent(root)
	if err != nil {
		t.Fatal(err)
	}
	if ptr.ActiveVersion != "v1.19.1" {
		t.Fatalf("active changed to %s", ptr.ActiveVersion)
	}
}

func TestActivateVersionRollsBackVersionAndRootEntriesBeforePointerCommit(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	seedMembers := make([]Member, 0, len(AllowedVersionMembers()))
	for _, name := range AllowedVersionMembers() {
		seedMembers = append(seedMembers, Member{Name: name, Path: writeTempMember(t, src, "old-"+name, "old-"+name)})
	}
	if err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     "v1.19.1",
		RequestID:   "seed-root-rollback",
		Members:     seedMembers,
	}); err != nil {
		t.Fatal(err)
	}
	oldLauncher := filepath.Join(root, LauncherBinaryName())
	if err := os.WriteFile(oldLauncher, []byte("old-launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	blockedAlias := filepath.Join(root, "blocked-alias")
	if err := os.Mkdir(blockedAlias, 0o755); err != nil {
		t.Fatal(err)
	}

	newMembers := make([]Member, 0, len(AllowedVersionMembers()))
	for _, name := range AllowedVersionMembers() {
		newMembers = append(newMembers, Member{Name: name, Path: writeTempMember(t, src, "new-"+name, "new-"+name)})
	}
	newLauncher := writeTempMember(t, src, "new-launcher", "new-launcher")
	err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     "v1.20.0",
		RequestID:   "root-rollback",
		Members:     newMembers,
		RootMembers: []Member{
			{Name: LauncherBinaryName(), Path: newLauncher},
			{Name: "blocked-alias", Path: newLauncher},
		},
		RequiredRootNames: []string{LauncherBinaryName(), "blocked-alias"},
	})
	if err == nil {
		t.Fatal("expected root entry publication failure")
	}
	ptr, readErr := ReadCurrent(root)
	if readErr != nil || ptr.ActiveVersion != "v1.19.1" {
		t.Fatalf("pointer=%+v err=%v", ptr, readErr)
	}
	body, readErr := os.ReadFile(oldLauncher)
	if readErr != nil || string(body) != "old-launcher" {
		t.Fatalf("launcher=%q err=%v", body, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, VersionsDirName, "v1.20.0")); !os.IsNotExist(statErr) {
		t.Fatalf("uncommitted version survived: %v", statErr)
	}
}

func TestActivateVersionRejectsExtraAndPathTraversalNames(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	members := []Member{
		{Name: DesktopBinaryName(), Path: writeTempMember(t, src, DesktopBinaryName(), "x")},
		{Name: CLIBinaryName(), Path: writeTempMember(t, src, CLIBinaryName(), "x")},
		{Name: UpdateHelperBinaryName(), Path: writeTempMember(t, src, UpdateHelperBinaryName(), "x")},
		{Name: "evil.exe", Path: writeTempMember(t, src, "evil.exe", "x")},
	}
	if err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     "v1.20.0",
		RequestID:   "extra",
		Members:     members,
	}); err == nil {
		t.Fatal("expected extra member rejection")
	}

	members = []Member{
		{Name: "../" + DesktopBinaryName(), Path: writeTempMember(t, src, "d", "x")},
		{Name: CLIBinaryName(), Path: writeTempMember(t, src, "c", "x")},
		{Name: UpdateHelperBinaryName(), Path: writeTempMember(t, src, "u", "x")},
	}
	if err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     "v1.20.0",
		RequestID:   "trav",
		Members:     members,
	}); err == nil {
		t.Fatal("expected traversal name rejection")
	}
}

func TestActivateVersionRejectsSymlinkSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege varies on Windows CI")
	}
	root := t.TempDir()
	src := t.TempDir()
	real := writeTempMember(t, src, "real", "body")
	link := filepath.Join(src, DesktopBinaryName())
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	members := []Member{
		{Name: DesktopBinaryName(), Path: link},
		{Name: CLIBinaryName(), Path: writeTempMember(t, src, CLIBinaryName(), "x")},
		{Name: UpdateHelperBinaryName(), Path: writeTempMember(t, src, UpdateHelperBinaryName(), "x")},
	}
	if err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     "v1.20.0",
		RequestID:   "symlink",
		Members:     members,
	}); err == nil {
		t.Fatal("expected symlink source rejection")
	}
	if HasCurrent(root) {
		t.Fatal("current.json must not be written after failed activation")
	}
}

func TestCleanupStaleStaging(t *testing.T) {
	root := t.TempDir()
	versions := filepath.Join(root, VersionsDirName)
	if err := os.MkdirAll(versions, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(versions, ".staging-v1.20.0-old")
	fresh := filepath.Join(versions, ".staging-v1.20.0-fresh")
	if err := os.Mkdir(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := CleanupStaleStaging(root, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old staging should be removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh staging should remain")
	}
}

func TestRetainPreviousVersionsKeepsOneRecent(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	makeVersion := func(v, body string) {
		members := make([]Member, 0, 3)
		for _, name := range AllowedVersionMembers() {
			members = append(members, Member{Name: name, Path: writeTempMember(t, src, v+"-"+name, body)})
		}
		if err := ActivateVersion(ActivationRequest{
			InstallRoot: root,
			Version:     v,
			RequestID:   v,
			Members:     members,
		}); err != nil {
			t.Fatal(err)
		}
	}
	makeVersion("v1.18.0", "a")
	// Age v1.18.0 so retention deletes it.
	oldDir := filepath.Join(root, "versions", "v1.18.0")
	oldTime := time.Now().Add(-30 * 24 * time.Hour)
	_ = os.Chtimes(oldDir, oldTime, oldTime)
	makeVersion("v1.19.0", "b")
	makeVersion("v1.20.0", "c")

	if err := RetainPreviousVersions(root, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "v1.20.0")); err != nil {
		t.Fatal("active version must remain")
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "v1.19.0")); err != nil {
		t.Fatal("one previous version within window should remain")
	}
	if _, err := os.Stat(filepath.Join(root, "versions", "v1.18.0")); !os.IsNotExist(err) {
		t.Fatal("aged previous version should be GC'd")
	}
	ptr, err := ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v1.20.0" {
		t.Fatalf("pointer = %+v err=%v", ptr, err)
	}
}
