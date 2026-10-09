package capdiag

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"reasonix/internal/contract/config"
)

// A live probe starts only what a session in this workspace would start. A
// server the user switched off, or a repository server whose declaration
// changed since it was enabled, must not run to be diagnosed.
func TestLiveProbeStartsOnlyWhatASessionWould(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the probe command is POSIX shell")
	}
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	off := config.PluginEntry{Name: "off", Command: "sh", Args: []string{"-c", "echo off >> " + marker}, Source: config.MCPSourceUserConfig}
	repo := config.PluginEntry{Name: "repo", Command: "sh", Args: []string{"-c", "echo repo >> " + marker}, Source: config.MCPSourceProjectMCPJSON}
	store := config.DefaultActivationStore()
	if err := store.SetServerEnabled(off, root, config.ActivationGlobal, false); err != nil {
		t.Fatal(err)
	}
	approved := repo
	approved.Args = []string{"-c", "true"}
	if err := store.SetServerEnabled(approved, root, config.ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	rep := &MCPReport{Servers: []MCPServerInfo{{Name: "off"}, {Name: "repo"}}}
	probeLiveMCP(rep, &config.Config{Plugins: []config.PluginEntry{off, repo}}, root, home, home, MinLiveTimeout)
	time.Sleep(200 * time.Millisecond)
	if body, err := os.ReadFile(marker); err == nil {
		t.Fatalf("live probe started servers a session would not: %q", body)
	}
	for _, s := range rep.Servers {
		if s.RuntimeStatus != "skipped" {
			t.Fatalf("%s runtime status = %q, want skipped", s.Name, s.RuntimeStatus)
		}
	}
}
