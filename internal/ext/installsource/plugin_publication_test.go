package installsource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/fileutil"
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

func TestPublicationRetryWindowRechecksTheApprovedBytes(t *testing.T) {
	tool, source := revisionPlugin(t)
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "replace": true}
	plan := execInstall(t, tool, args)
	calls := 0
	restore := fileutil.SetRenameForTest(func(oldpath, newpath string) error {
		calls++
		entries, _ := os.ReadDir(pluginpkg.PluginsDir(tool.reasonixHome))
		for _, entry := range entries {
			if entry.IsDir() {
				writeFile(t, filepath.Join(pluginpkg.PluginsDir(tool.reasonixHome), entry.Name(), "skills", "neutral", "SKILL.md"), "ALTERED IN THE RETRY WINDOW")
			}
		}
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errors.New("transient sharing violation")}
	})
	t.Cleanup(restore)
	args["apply"], args["planId"] = true, plan.PlanID
	done := execInstall(t, tool, args)
	if done.Status != "failed" || len(done.Actions) != 1 || done.Actions[0].ErrorCode != "install.digest_mismatch" {
		t.Fatalf("response = %+v, want a failed action with the digest-mismatch code", done)
	}
	if calls != 1 {
		t.Fatalf("rename attempts = %d, want 1: a failed re-check must stop the retries", calls)
	}
	if st, err := pluginpkg.LoadState(tool.reasonixHome); err != nil || len(st.Plugins) != 0 {
		t.Fatalf("altered tree registered: %+v, %v", st, err)
	}
}
