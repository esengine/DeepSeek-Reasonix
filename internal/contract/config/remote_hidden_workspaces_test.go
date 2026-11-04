package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
	"reasonix/internal/base/testenv"
)

func TestRemoteHiddenWorkspacesRoundTrip(t *testing.T) {
	isolateUserConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	legacy := "[[remote.hosts]]\nname = \"box\"\nhost = \"example.test\"\nworkspace = \"/keep\"\n"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadForEdit(path)
	entry, _ := cfg.RemoteHost("box")
	if len(entry.HiddenWorkspaces) != 0 {
		t.Fatal("old config hides directories")
	}
	if !cfg.RemoveRemoteWorkspace("box", "/root") {
		t.Fatal("unbooked directory was not removed")
	}
	if cfg.RemoveRemoteWorkspace("box", "/root") {
		t.Fatal("repeated removal changed configuration")
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	cfg = LoadForEdit(path)
	entry, _ = cfg.RemoteHost("box")
	if !reflect.DeepEqual(entry.HiddenWorkspaces, []string{"/root"}) || entry.DefaultWorkspace() != "/keep" {
		t.Fatalf("reload = %+v", entry)
	}
	var oldReader struct {
		Remote struct {
			Hosts []struct{ Name, Host, Workspace string }
		}
	}
	if _, err := toml.DecodeFile(path, &oldReader); err != nil {
		t.Fatal(err)
	}
	if len(oldReader.Remote.Hosts) != 1 || oldReader.Remote.Hosts[0].Workspace != "/keep" {
		t.Fatalf("old reader lost host: %+v", oldReader)
	}
	cloned := cfg.Remote.Clone()
	cloned.Hosts[0].HiddenWorkspaces[0] = "/other"
	if cfg.Remote.Hosts[0].HiddenWorkspaces[0] != "/root" {
		t.Fatal("clone aliases hidden directories")
	}
	if !cfg.AddRemoteWorkspace("box", "/root") {
		t.Fatal("re-add did not restore directory")
	}
	entry, _ = cfg.RemoteHost("box")
	if len(entry.HiddenWorkspaces) != 0 {
		t.Fatalf("re-add remains hidden: %+v", entry)
	}
}
