package skill_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/skill"
)

func TestVideoProductionBuiltinRegisteredInline(t *testing.T) {
	store := skill.New(skill.Options{HomeDir: testenv.TempDir(t)})
	sk, ok := store.Read("video-production")
	if !ok {
		t.Fatal("video-production must be registered as a builtin")
	}
	if sk.Scope != skill.ScopeBuiltin || sk.RunAs != skill.RunInline {
		t.Fatalf("scope %s runAs %s", sk.Scope, sk.RunAs)
	}
	for _, rule := range []string{"manifest", "submission_unknown", "download_artifact", "check_video", "unverified", "shot_id"} {
		if !strings.Contains(sk.Body, rule) {
			t.Errorf("body is missing %q", rule)
		}
	}
}

// An embedded skill ships as one file: a path into the Reasonix repo names
// nothing in the user's workspace, and the model spends turns looking for it.
func TestVideoProductionNamesNoFileItDoesNotShip(t *testing.T) {
	sk, _ := skill.New(skill.Options{HomeDir: testenv.TempDir(t)}).Read("video-production")
	if strings.Contains(sk.Body, "docs/") {
		t.Fatal("the skill body points at a repo document the workspace does not have")
	}
	for _, field := range []string{"production_id", "artifact_id", "superseded_by", "brief_revision", "required_reviews"} {
		if !strings.Contains(sk.Body, field) {
			t.Errorf("the manifest field %q is not spelled out in the skill", field)
		}
	}
}

func TestVideoProductionIndexCarriesOnlyItsLine(t *testing.T) {
	store := skill.New(skill.Options{HomeDir: testenv.TempDir(t)})
	idx := skill.IndexBlock(store.List())
	if c := strings.Count(idx, "- video-production"); c != 1 {
		t.Fatalf("index lines for video-production = %d, want 1", c)
	}
	if strings.Contains(idx, "Never claim a capability") {
		t.Fatal("skill body leaked into the index")
	}
	if skill.IndexBlock(store.List()) != idx {
		t.Fatal("index is not byte-stable across calls")
	}
}

func TestVideoProductionOverriddenByProject(t *testing.T) {
	home, root := testenv.TempDir(t), testenv.TempDir(t)
	dir := filepath.Join(root, ".reasonix", "skills", "video-production")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: video-production\ndescription: house style\n---\nOURS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sk, ok := skill.New(skill.Options{HomeDir: home, ProjectRoot: root}).Read("video-production")
	if !ok || sk.Scope == skill.ScopeBuiltin || !strings.Contains(sk.Body, "OURS") {
		t.Fatalf("project skill did not win: %+v", sk)
	}
}
