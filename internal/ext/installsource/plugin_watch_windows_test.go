package installsource

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func holdPluginDirectoryWatch(t *testing.T, dir string) {
	t.Helper()
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(path, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatal(err)
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(handle)
		t.Fatal(err)
	}
	buffer := make([]byte, 4096)
	overlap := &windows.Overlapped{HEvent: event}
	if err := windows.ReadDirectoryChanges(handle, &buffer[0], uint32(len(buffer)), false,
		windows.FILE_NOTIFY_CHANGE_FILE_NAME|windows.FILE_NOTIFY_CHANGE_DIR_NAME|windows.FILE_NOTIFY_CHANGE_LAST_WRITE,
		nil, overlap, 0); err != nil && !errors.Is(err, windows.ERROR_IO_PENDING) {
		windows.CloseHandle(event)
		windows.CloseHandle(handle)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = windows.CancelIoEx(handle, overlap)
		var transferred uint32
		_ = windows.GetOverlappedResult(handle, overlap, &transferred, true)
		_ = windows.CloseHandle(handle)
		_ = windows.CloseHandle(event)
		runtime.KeepAlive(buffer)
		runtime.KeepAlive(overlap)
	})
}

func watchedPluginFixture(t *testing.T) (*Tool, string, string) {
	t.Helper()
	home := testenv.TempDir(t)
	tool := NewTool(Options{HomeDir: home, ProjectRoot: home})
	source := filepath.Join(testenv.TempDir(t), "source")
	writeFile(t, filepath.Join(source, ".claude-plugin", "plugin.json"), `{"name":"watch-probe","version":"1.0.0"}`)
	writeFile(t, filepath.Join(source, "skills", "probe", "SKILL.md"), "---\nname: probe\ndescription: Neutral probe\n---\nOLD BODY")
	writeFile(t, filepath.Join(source, "skills", "probe", "removed.txt"), "OLD ASSET")
	act := plannedPluginCopy(t, tool, source)
	if err := tool.applyInstallPluginPackage(t.Context(), request{}, &act); err != nil {
		t.Fatal(err)
	}
	old := act.Target
	holdPluginDirectoryWatch(t, filepath.Join(old, "skills"))
	holdPluginDirectoryWatch(t, filepath.Join(old, "skills", "probe"))
	if err := os.Rename(old, old+".probe"); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		if err == nil {
			_ = os.Rename(old+".probe", old)
		}
		t.Fatalf("active watch must deny ancestor rename, got %v", err)
	}
	writeFile(t, filepath.Join(source, ".claude-plugin", "plugin.json"), `{"name":"watch-probe","version":"2.0.0"}`)
	writeFile(t, filepath.Join(source, "skills", "probe", "SKILL.md"), "---\nname: probe\ndescription: Neutral probe\n---\nNEW BODY")
	if err := os.Remove(filepath.Join(source, "skills", "probe", "removed.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "skills", "probe", "added.txt"), "NEW ASSET")
	return tool, source, old
}

func TestWindowsPluginWatchedUpdateRollbackOnStateWriteError(t *testing.T) {
	tool, source, old := watchedPluginFixture(t)
	statePath := pluginpkg.StatePath(tool.reasonixHome)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	path, err := windows.UTF16PtrFromString(statePath)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(handle) })
	previous := fileutil.CrashPoint
	t.Cleanup(func() { fileutil.CrashPoint = previous })
	reachedPublication := false
	fileutil.CrashPoint = func(op, path string) {
		if op == "atomic-write" && path == statePath {
			reachedPublication = true
		}
	}
	act := plannedPluginCopy(t, tool, source)
	if err := tool.applyInstallPluginPackage(t.Context(), request{Replace: true}, &act); err == nil {
		t.Fatal("locked state file must refuse publication")
	}
	if !reachedPublication {
		t.Fatal("update failed before state publication")
	}
	after, err := os.ReadFile(statePath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("state changed on failed publication: %q, %v", after, err)
	}
	entries, err := os.ReadDir(filepath.Dir(old))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(old) {
		t.Fatalf("failed update left trees: %v, %v", entries, err)
	}
}

func TestWindowsPluginGenerationRequiresExplicitReplace(t *testing.T) {
	home := testenv.TempDir(t)
	tool := NewTool(Options{HomeDir: home, ProjectRoot: home})
	source := testenv.TempDir(t)
	writeFile(t, filepath.Join(source, ".claude-plugin", "plugin.json"), `{"name":"generation-probe"}`)
	writeFile(t, filepath.Join(source, "skills", "probe", "SKILL.md"), "---\nname: probe\ndescription: Neutral probe\n---\nBODY")
	act := plannedPluginCopy(t, tool, source)
	if err := tool.applyInstallPluginPackage(t.Context(), request{Replace: true}, &act); err != nil {
		t.Fatal(err)
	}
	if err := tool.applyInstallPluginPackage(t.Context(), request{}, &act); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("unapproved replacement = %v", err)
	}
	if err := tool.applyInstallPluginPackage(t.Context(), request{Replace: true}, &act); err != nil {
		t.Fatalf("repeated generation replacement = %v", err)
	}
}

func TestWindowsPluginUpdateWithExternalDirectoryWatch(t *testing.T) {
	tool, source, old := watchedPluginFixture(t)
	act := plannedPluginCopy(t, tool, source)
	if err := tool.applyInstallPluginPackage(t.Context(), request{Replace: true}, &act); err != nil {
		t.Fatalf("update with active external watch: %v", err)
	}
	st, err := pluginpkg.LoadState(tool.reasonixHome)
	if err != nil || len(st.Plugins) != 1 {
		t.Fatalf("state = %+v, error = %v", st, err)
	}
	root := pluginpkg.ResolveRoot(tool.reasonixHome, st.Plugins[0].Root)
	if root == old || root != act.Target || st.Plugins[0].Version != "2.0.0" {
		t.Fatalf("replacement root = %s, action = %+v, state = %+v", root, act, st)
	}
	for path, want := range map[string]string{root: "NEW BODY", old: "OLD BODY"} {
		body, err := os.ReadFile(filepath.Join(path, "skills", "probe", "SKILL.md"))
		if err != nil || string(body) != "---\nname: probe\ndescription: Neutral probe\n---\n"+want {
			t.Fatalf("tree %s = %q, error = %v", path, body, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "probe", "removed.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed asset remains active: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(root, "skills", "probe", "added.txt")); err != nil || string(body) != "NEW ASSET" {
		t.Fatalf("added asset = %q, error=%v", body, err)
	}
}

func TestWindowsPluginWatchedUpdateRollbackAtPublication(t *testing.T) {
	tool, source, old := watchedPluginFixture(t)
	before, err := os.ReadFile(pluginpkg.StatePath(tool.reasonixHome))
	if err != nil {
		t.Fatal(err)
	}
	marker := errors.New("injected publication failure")
	previous := fileutil.CrashPoint
	t.Cleanup(func() { fileutil.CrashPoint = previous })
	fileutil.CrashPoint = func(op, path string) {
		if op == "atomic-write" && path == pluginpkg.StatePath(tool.reasonixHome) {
			panic(marker)
		}
	}
	var caught any
	func() {
		defer func() { caught = recover() }()
		act := plannedPluginCopy(t, tool, source)
		if err := tool.applyInstallPluginPackage(t.Context(), request{Replace: true}, &act); err != nil {
			t.Fatalf("update did not reach publication fault: %v", err)
		}
	}()
	if caught != marker {
		t.Fatalf("publication fault = %v", caught)
	}
	after, err := os.ReadFile(pluginpkg.StatePath(tool.reasonixHome))
	if err != nil || string(after) != string(before) {
		t.Fatalf("state changed on failed publication: %q, %v", after, err)
	}
	entries, err := os.ReadDir(filepath.Dir(old))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(old) {
		t.Fatalf("failed update left trees: %v, %v", entries, err)
	}
	body, err := os.ReadFile(filepath.Join(old, "skills", "probe", "SKILL.md"))
	if err != nil || string(body) != "---\nname: probe\ndescription: Neutral probe\n---\nOLD BODY" {
		t.Fatalf("old tree changed: %q, %v", body, err)
	}
}
