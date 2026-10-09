package installsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/ext/pluginpkg"
)

func forceSiblingPublication(t *testing.T) {
	t.Helper()
	prev := siblingPublication
	siblingPublication = true
	t.Cleanup(func() { siblingPublication = prev })
}

func TestReplaceKeepsInPlaceSwapWhereDirectoriesCanBeRenamed(t *testing.T) {
	native := siblingPublication
	if native {
		t.Skip("this platform publishes a replacement beside the old tree")
	}
	tool, source := revisionPlugin(t)
	siblingPublication = native
	act := plannedPluginCopy(t, tool, source)
	if err := tool.applyInstallPluginPackage(t.Context(), request{}, &act); err != nil {
		t.Fatal(err)
	}
	first := act.Target
	writeFile(t, filepath.Join(source, "skills", "neutral", "SKILL.md"), "---\nname: neutral\ndescription: Neutral fixture\n---\nNEW")
	act = plannedPluginCopy(t, tool, source)
	if err := tool.applyInstallPluginPackage(t.Context(), request{Replace: true}, &act); err != nil {
		t.Fatal(err)
	}
	if act.Target != first {
		t.Fatalf("replacement published at %s, want the original %s", act.Target, first)
	}
	installed, _, err := pluginpkg.FindInstalled(tool.reasonixHome, act.Name)
	if err != nil || pluginpkg.ResolveRoot(tool.reasonixHome, installed.Root) != first {
		t.Fatalf("registered root = %q, want %s (%v)", installed.Root, first, err)
	}
	if body, _ := os.ReadFile(filepath.Join(first, "skills", "neutral", "SKILL.md")); !strings.Contains(string(body), "NEW") {
		t.Fatalf("replaced tree holds %q", body)
	}
	entries, err := os.ReadDir(pluginpkg.PluginsDir(tool.reasonixHome))
	if err != nil || len(entries) != 1 {
		t.Fatalf("plugins dir holds %d entries (%v), want only the installed tree", len(entries), err)
	}
}
