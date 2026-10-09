package installsource

import (
	"errors"
	"os"
	"path/filepath"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
	"testing"
)

func TestRevisionPublicationRefusalKeepsPreviousGeneration(t *testing.T) {
	tool, source := revisionPlugin(t)
	first := execInstall(t, tool, map[string]any{"source": source, "kind": "plugin", "apply": true})
	if first.Applied {
		t.Fatal("unticketed install wrote")
	}
	first = execInstall(t, tool, map[string]any{"source": source, "kind": "plugin", "apply": true, "planId": first.PlanID})
	old := first.Actions[0].Target
	before, err := os.ReadFile(pluginpkg.StatePath(tool.reasonixHome))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "skills", "neutral", "SKILL.md"), "---\nname: neutral\ndescription: Neutral fixture\n---\nREPLACEMENT")
	args := map[string]any{"source": source, "kind": "plugin", "replace": true}
	plan := execInstall(t, tool, args)
	prior := fileutil.CrashPoint
	defer func() { fileutil.CrashPoint = prior }()
	fileutil.CrashPoint = func(op, path string) {
		if op != "atomic-write" || path != pluginpkg.StatePath(tool.reasonixHome) {
			return
		}
		entries, err := os.ReadDir(filepath.Dir(old))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			root := filepath.Join(filepath.Dir(old), entry.Name())
			if root != old && entry.IsDir() {
				writeFile(t, filepath.Join(root, "skills", "neutral", "SKILL.md"), "MUTATED")
			}
		}
	}
	args["apply"], args["planId"] = true, plan.PlanID
	done := execInstall(t, tool, args)
	if done.Status != "failed" || done.Actions[0].ErrorCode != "install.digest_mismatch" {
		t.Fatalf("refusal: %+v", done)
	}
	after, err := os.ReadFile(pluginpkg.StatePath(tool.reasonixHome))
	if err != nil || string(before) != string(after) {
		t.Fatalf("registry changed: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(old, "skills", "neutral", "SKILL.md"))
	if err != nil || string(body) != "---\nname: neutral\ndescription: Neutral fixture\n---\nAPPROVED" {
		t.Fatalf("old bytes lost: %q, %v", body, err)
	}
	entries, err := os.ReadDir(filepath.Dir(old))
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed generation remains: %v, %v", entries, err)
	}
}

func TestRevisionDigestRejectsUnreadableAndOversizedCopies(t *testing.T) {
	root := testenv.TempDir(t)
	if _, err := copiedPluginDigest(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing root: %v", err)
	}
	if err := verifyPluginDigest(root, "different"); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("different digest: %v", err)
	}
	file := filepath.Join(root, "large")
	writeFile(t, file, "")
	if err := os.Truncate(file, tarballTotalLimit+1); err != nil {
		t.Fatal(err)
	}
	if _, err := copiedPluginDigest(root); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("oversized material: %v", err)
	}
	if _, _, _, err := snapshotPlugin(pluginpkg.Package{Root: root}); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("oversized snapshot: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := snapshotPlugin(pluginpkg.Package{Root: filepath.Join(root, "missing")}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing snapshot: %v", err)
	}
	if _, _, _, err := snapshotPlugin(pluginpkg.Package{Root: root}); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("manifestless copy: %v", err)
	}
}
