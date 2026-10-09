package installsource

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestNetworkPathSourceIsRefusedBeforeAnyLookup(t *testing.T) {
	prev := fileutil.HostIsWindows
	fileutil.HostIsWindows = true
	t.Cleanup(func() { fileutil.HostIsWindows = prev })

	project, home := testenv.TempDir(t), testenv.TempDir(t)
	skills := filepath.Join(project, "skills", "alpha")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skills, "SKILL.md"), "---\nname: alpha\ndescription: d\n---\nbody\n")
	slashed := filepath.ToSlash(filepath.Join(project, "skills"))
	if !strings.HasPrefix(slashed, "/") {
		t.Skip("needs a rooted local path")
	}
	source := "/" + slashed // a POSIX double slash names the same local directory

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	raw, _ := json.Marshal(map[string]any{"source": source, "kind": "skill"})
	out, err := tl.Execute(context.Background(), raw)
	var refusal tool.Refusal
	if err == nil || !errors.As(err, &refusal) || refusal.Code != fileutil.CodeNetworkPathOutsideScope {
		t.Fatalf("plan of a network-path source returned %q, %v; want a refusal with %q", out, err, fileutil.CodeNetworkPathOutsideScope)
	}

	local, _ := json.Marshal(map[string]any{"source": filepath.Join(project, "skills"), "kind": "skill"})
	if _, err := tl.Execute(context.Background(), local); err != nil {
		t.Fatalf("local source refused: %v", err)
	}
}

func TestNetworkSourceRefusalKeepsItsCauseAtTheToolResult(t *testing.T) {
	prev := fileutil.HostIsWindows
	fileutil.HostIsWindows = true
	t.Cleanup(func() { fileutil.HostIsWindows = prev })
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	raw, _ := json.Marshal(map[string]any{"source": "//evil/share/skills", "kind": "skill"})
	_, err := tl.Execute(context.Background(), raw)
	if err == nil {
		t.Fatal("network source accepted")
	}
	for _, want := range []string{fileutil.CodeNetworkPathOutsideScope, "network path"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error text %q does not carry %q to the model", err.Error(), want)
		}
	}
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != fileutil.CodeNetworkPathOutsideScope {
		t.Errorf("refusal identity lost: %v", err)
	}
}
