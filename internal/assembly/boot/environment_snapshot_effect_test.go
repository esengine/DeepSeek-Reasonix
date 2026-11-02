package boot

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/platform/environment"
)

func environmentSnapshotPrompt(t *testing.T, home, dir string) string {
	t.Helper()
	c, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, resolvedShell: pinnedEffectShell(), Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return systemMessage(c.History())
}

func TestBuildEnvironmentSnapshotHelper(t *testing.T) {
	args := flag.Args()
	if len(args) != 3 {
		return
	}
	if err := os.WriteFile(args[2], []byte(environmentSnapshotPrompt(t, args[0], args[1])), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBuildKeepsEnvironmentSnapshotAcrossOtherHomeRefresh(t *testing.T) {
	home, dir := statedBootHome(t), testenv.TempDir(t)
	tool := filepath.Join(testenv.TempDir(t), "probe-tool")
	if runtime.GOOS == "windows" {
		tool += ".bat"
	}
	writeTool := func(version string) {
		t.Helper()
		body := "#!/bin/sh\nprintf '%s\\n' '" + version + "'\n"
		if runtime.GOOS == "windows" {
			body = "@echo " + version + "\r\n"
		}
		if err := os.WriteFile(tool, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTool("probe version one")
	var config strings.Builder
	config.WriteString("default_model = 'test'\n[[providers]]\nname = 'test'\nkind = 'openai'\nmodel = 'test'\nbase_url = 'https://example.invalid'\n[environment.tools]\n")
	for _, command := range environment.DefaultProbes() {
		fmt.Fprintf(&config, "%s = %q\n", strings.Fields(command)[0], tool)
	}
	writeFile(t, dir, "reasonix.toml", config.String())
	approveWorkspaceAt(t, home, dir)
	promptFile := filepath.Join(testenv.TempDir(t), "prompt.txt")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBuildEnvironmentSnapshotHelper$", "--", home, dir, promptFile)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initial process: %v\n%s", err, output)
	}
	initial, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(initial), "probe version one") {
		t.Fatal("initial build did not render the fixture's tool versions")
	}
	writeTool("probe version two")
	otherHome := statedBootHome(t)
	approveWorkspaceAt(t, otherHome, dir)
	if other := environmentSnapshotPrompt(t, otherHome, dir); !strings.Contains(other, "probe version two") {
		t.Fatal("new home did not probe the changed tool")
	}
	if rebuilt := environmentSnapshotPrompt(t, home, dir); rebuilt != string(initial) {
		t.Fatalf("another home's shared probes changed the persisted prefix: %q", firstDivergence(string(initial), rebuilt))
	}
}
