package installsource

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func revisionPlugin(t *testing.T) (*Tool, string) {
	t.Helper()
	forceSiblingPublication(t)
	home := testenv.TempDir(t)
	source := testenv.TempDir(t)
	writeFile(t, filepath.Join(source, ".claude-plugin", "plugin.json"), `{"name":"approved"}`)
	writeFile(t, filepath.Join(source, "skills", "neutral", "SKILL.md"), "---\nname: neutral\ndescription: Neutral fixture\n---\nAPPROVED")
	return NewTool(Options{ProjectRoot: home, HomeDir: home, RequireApprovedPlan: true}), source
}

func TestRevisionCopyApprovalBindsAllBytes(t *testing.T) {
	for _, resource := range []string{"skills/neutral/SKILL.md", "hooks/run.ps1", "prompts/example.md", "themes/theme.json", "bin/runtime.exe"} {
		t.Run(resource, func(t *testing.T) {
			tool, source := revisionPlugin(t)
			path := filepath.Join(source, filepath.FromSlash(resource))
			if resource != "skills/neutral/SKILL.md" {
				writeFile(t, path, "APPROVED")
			}
			args := map[string]any{"source": source, "kind": "plugin", "mode": "copy"}
			plan := execInstall(t, tool, args)
			if !IsContentDigest(plan.ContentDigest) {
				t.Error("copy preview must expose a content digest")
			}
			writeFile(t, path, "---\nname: neutral\ndescription: Neutral fixture\n---\nCHANGED")
			args["apply"], args["planId"] = true, plan.PlanID
			if _, err := execRaw(t, tool, args); !errors.Is(err, ErrApprovalDenied) {
				t.Errorf("changed bytes with original ticket: %v", err)
			}
			args["expectDigest"] = plan.ContentDigest
			if _, err := execRaw(t, tool, args); !errors.Is(err, ErrDigestMismatch) {
				t.Errorf("changed pinned bytes: %v", err)
			}
			st, err := pluginpkg.LoadState(tool.reasonixHome)
			if err != nil || len(st.Plugins) != 0 {
				t.Fatalf("refused content published: %+v, %v", st, err)
			}
		})
	}
}

func TestRevisionCopyDigestStableAndLinkMutable(t *testing.T) {
	tool, source := revisionPlugin(t)
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy"}
	plan := execInstall(t, tool, args)
	writeFile(t, filepath.Join(source, ".git", "ignored"), "VCS metadata")
	again := execInstall(t, tool, args)
	if plan.ContentDigest == "" || again.ContentDigest != plan.ContentDigest || again.PlanID != plan.PlanID {
		t.Fatal("VCS metadata changed approval or missing digest")
	}
	args["apply"], args["planId"], args["expectDigest"] = true, plan.PlanID, plan.ContentDigest
	if done, err := execRaw(t, tool, args); err != nil || done.Status != "done" {
		t.Fatalf("unchanged approved copy: %+v, %v", done, err)
	}
	link := execInstall(t, tool, map[string]any{"source": source, "kind": "plugin", "mode": "link"})
	if link.ContentDigest != "" {
		t.Fatal("linked source must remain unpinnable")
	}
}

func TestRevisionPublicationMutationFailsClosed(t *testing.T) {
	tool, source := revisionPlugin(t)
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "replace": true}
	plan := execInstall(t, tool, args)
	previous := fileutil.CrashPoint
	t.Cleanup(func() { fileutil.CrashPoint = previous })
	fileutil.CrashPoint = func(op, path string) {
		if op != "atomic-write" || path != pluginpkg.StatePath(tool.reasonixHome) {
			return
		}
		entries, err := os.ReadDir(pluginpkg.PluginsDir(tool.reasonixHome))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				writeFile(t, filepath.Join(pluginpkg.PluginsDir(tool.reasonixHome), entry.Name(), "skills", "neutral", "SKILL.md"), "ALTERED AFTER VALIDATION")
			}
		}
	}
	args["apply"], args["planId"] = true, plan.PlanID
	done := execInstall(t, tool, args)
	if done.Status != "failed" {
		t.Fatalf("mutated staging published: %+v", done)
	}
	if len(done.Actions) != 1 || done.Actions[0].Error == "" {
		t.Fatalf("missing failed action: %+v", done)
	}
	st, err := pluginpkg.LoadState(tool.reasonixHome)
	if err != nil || len(st.Plugins) != 0 {
		t.Fatalf("mutated tree registered: %+v, %v", st, err)
	}
}
