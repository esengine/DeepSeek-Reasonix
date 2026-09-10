package installlayout

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var shellTreeNames = []string{
	AppShellDirName + "/" + ShellExecutableName(),
	AppShellDirName + "/resources/app.asar",
	AppShellDirName + "/locales/en-US.pak",
}

func treeMemberNames() []string {
	return append(AllowedVersionMembers(), shellTreeNames...)
}

func writeTreeMembers(t *testing.T, src, body string) []Member {
	t.Helper()
	names := treeMemberNames()
	members := make([]Member, 0, len(names))
	for _, name := range names {
		path := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body+"-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
		members = append(members, Member{Name: name, Path: path})
	}
	return members
}

func activateTree(t *testing.T, root, version, requestID string, members []Member) error {
	t.Helper()
	return ActivateVersion(ActivationRequest{
		InstallRoot:   root,
		Version:       version,
		RequestID:     requestID,
		Members:       members,
		RequiredNames: treeMemberNames(),
	})
}

func TestActivateVersionPublishesAppShellTree(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	if err := activateTree(t, root, "v1.30.0", "tree-publish", writeTreeMembers(t, src, "new")); err != nil {
		t.Fatal(err)
	}
	versionDir := filepath.Join(root, VersionsDirName, "v1.30.0")
	for _, name := range shellTreeNames {
		raw, err := os.ReadFile(filepath.Join(versionDir, filepath.FromSlash(name)))
		if err != nil || string(raw) != "new-"+name {
			t.Fatalf("tree member %s = %q err=%v", name, raw, err)
		}
	}
	desktop, err := ActiveDesktopPath(root)
	if err != nil || filepath.Base(desktop) != DesktopBinaryName() {
		t.Fatalf("active desktop = %s err=%v", desktop, err)
	}
	relaunch, err := StableRelaunchPath(root)
	if err != nil || relaunch != desktop {
		t.Fatalf("stable relaunch = %s err=%v, want %s", relaunch, err, desktop)
	}
}

func TestActivateVersionTreeFailureKeepsOldPointerAndTree(t *testing.T) {
	root := t.TempDir()
	if err := activateTree(t, root, "v1.30.0", "tree-seed", writeTreeMembers(t, t.TempDir(), "old")); err != nil {
		t.Fatal(err)
	}
	badSrc := t.TempDir()
	members := writeTreeMembers(t, badSrc, "new")
	asar := filepath.Join(badSrc, AppShellDirName, "resources", "app.asar")
	if err := os.Remove(asar); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(asar, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := activateTree(t, root, "v1.31.0", "tree-fault", members); err == nil {
		t.Fatal("expected activation failure for a non-regular tree source")
	}
	ptr, err := ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v1.30.0" {
		t.Fatalf("pointer=%+v err=%v", ptr, err)
	}
	if _, err := os.Stat(filepath.Join(root, VersionsDirName, "v1.31.0")); !os.IsNotExist(err) {
		t.Fatalf("failed version directory survived: %v", err)
	}
	shell := filepath.Join(root, VersionsDirName, "v1.30.0", AppShellDirName, ShellExecutableName())
	if raw, err := os.ReadFile(shell); err != nil || !strings.HasPrefix(string(raw), "old-") {
		t.Fatalf("active shell = %q err=%v", raw, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, VersionsDirName))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".staging-") {
			t.Fatalf("staging directory %s survived the failed activation", entry.Name())
		}
	}
}

func TestActivateVersionRejectsTreeMembersOutsideWhitelist(t *testing.T) {
	root := t.TempDir()
	members := writeTreeMembers(t, t.TempDir(), "x")
	if err := ActivateVersion(ActivationRequest{
		InstallRoot: root,
		Version:     "v1.30.0",
		RequestID:   "tree-default-list",
		Members:     members,
	}); err == nil {
		t.Fatal("default whitelist must reject app/ members")
	}
	flat := members[:len(AllowedVersionMembers())]
	if err := ActivateVersion(ActivationRequest{
		InstallRoot:       root,
		Version:           "v1.30.0",
		RequestID:         "tree-root",
		Members:           flat,
		RootMembers:       []Member{{Name: shellTreeNames[0], Path: members[len(flat)].Path}},
		RequiredRootNames: []string{shellTreeNames[0]},
	}); err == nil {
		t.Fatal("root entries must stay flat")
	}
	if HasCurrent(root) {
		t.Fatal("current.json must not exist after rejected activations")
	}
}

func TestActivateVersionRejectsTreeTraversalNames(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	members := writeTreeMembers(t, src, "x")
	for _, bad := range []string{
		"../" + AppShellDirName + "/x",
		AppShellDirName + "/../" + DesktopBinaryName(),
		AppShellDirName + "/./x",
		AppShellDirName + "//x",
		AppShellDirName + "/x/",
		AppShellDirName + `\x`,
		"/" + AppShellDirName + "/x",
		"lib/x",
		AppShellDirName + "/c:x",
		" " + AppShellDirName + "/x",
	} {
		req := ActivationRequest{
			InstallRoot:   root,
			Version:       "v1.30.0",
			RequestID:     "tree-traversal",
			Members:       append(append([]Member(nil), members...), Member{Name: bad, Path: members[0].Path}),
			RequiredNames: append(treeMemberNames(), bad),
		}
		if err := ActivateVersion(req); err == nil {
			t.Fatalf("member name %q was accepted", bad)
		}
		if err := ValidateMemberName(bad); err == nil {
			t.Fatalf("ValidateMemberName(%q) accepted", bad)
		}
	}
	for _, good := range append([]string{DesktopBinaryName()}, shellTreeNames...) {
		if err := ValidateMemberName(good); err != nil {
			t.Fatalf("ValidateMemberName(%q) = %v", good, err)
		}
	}
	if HasCurrent(root) {
		t.Fatal("current.json must not exist after rejected activations")
	}
}

func TestActivateVersionRejectsTreeSymlinkSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege varies on Windows CI")
	}
	root := t.TempDir()
	src := t.TempDir()
	members := writeTreeMembers(t, src, "x")
	shell := filepath.Join(src, AppShellDirName, ShellExecutableName())
	real := writeTempMember(t, src, "real-shell", "body")
	if err := os.Remove(shell); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, shell); err != nil {
		t.Fatal(err)
	}
	if err := activateTree(t, root, "v1.30.0", "tree-symlink", members); err == nil {
		t.Fatal("expected symlink tree source rejection")
	}
	if HasCurrent(root) {
		t.Fatal("current.json must not be written after failed activation")
	}
}
