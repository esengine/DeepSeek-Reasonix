package memory

import (
	"strings"
	"testing"
)

func TestNormalizeSubjectKey(t *testing.T) {
	for in, want := range map[string]string{
		"project.package_manager":  "project.package_manager",
		" Project.Package Manager": "project.package_manager",
		"project..release-branch":  "project.release-branch",
		"项目.包管理":                   "",
		"...":                      "",
	} {
		if got := NormalizeSubjectKey(in); got != want {
			t.Fatalf("NormalizeSubjectKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// The npm -> pnpm scenario: a second fact answering a held subject is
// rejected with directions to the holder, and updating the holder lands the
// new value as a revision.
func TestSubjectKeyRejectsSecondActiveValueWithDirections(t *testing.T) {
	store := recallTestStore(t)
	saved, err := store.SaveWithOptions(Memory{
		Name: "package-manager", Title: "Package manager",
		Description: "This project uses npm", SubjectKey: "project.package_manager",
		Body: "This project uses npm.",
	}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.SaveWithOptions(Memory{
		Name: "dependency-tooling", Title: "Dependency tooling",
		Description: "We migrated to pnpm", SubjectKey: "project.package_manager",
		Body: "We migrated to pnpm.",
	}, SaveOptions{})
	if err == nil {
		t.Fatal("a second active value for a held subject must be rejected")
	}
	for _, want := range []string{saved.Memory.ID, "update that id", "uses npm"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("conflict error must carry %q for the model to act on, got: %v", want, err)
		}
	}

	updated, err := store.SaveWithOptions(Memory{
		ID: saved.Memory.ID, Description: "We migrated to pnpm", Body: "We migrated to pnpm.",
	}, SaveOptions{})
	if err != nil || updated.Memory.Revision != 2 {
		t.Fatalf("updating the holder must land the new value as a revision: %+v %v", updated.Memory, err)
	}
	if NormalizeSubjectKey(updated.Memory.SubjectKey) != "project.package_manager" {
		t.Fatalf("subject key must inherit on update, got %+v", updated.Memory)
	}
}

func TestSubjectKeyScopesAreIndependentAndProjectShadowsGlobal(t *testing.T) {
	store := recallTestStore(t)
	recallTestWrite(t, store.GlobalDir, Memory{
		ID: "mem-global-style", Name: "response-style-global", Title: "Response style",
		Description: "Prefers detailed answers", Type: TypeUser, Scope: FactScopeGlobal,
		Activation: ActivationRelevant, SubjectKey: "user.response_style",
		Body: "Prefers long, detailed answers.",
	})
	if _, err := store.SaveWithOptions(Memory{
		Name: "response-style-here", Title: "Response style for this repo",
		Description: "结论先行，宁短勿长", Scope: FactScopeProject,
		SubjectKey: "user.response_style", Keywords: "response style answers",
		Body: "结论先行，宁短勿长。",
	}, SaveOptions{}); err != nil {
		t.Fatalf("the same subject in another scope is an override, not a conflict: %v", err)
	}

	result := AutoRecall(store, "response style preference for answers", RecallOptions{})
	for _, hit := range result.Hits {
		if hit.Memory.ID == "mem-global-style" {
			t.Fatalf("project subject holder must shadow the global one in recall: %+v", result.Hits)
		}
	}
}

func TestSubjectConflictSkipsSelfAndUnkeyedFacts(t *testing.T) {
	store := recallTestStore(t)
	saved, err := store.SaveWithOptions(Memory{
		Name: "release-branch", Description: "release/1.21",
		SubjectKey: "project.release_branch", Body: "release/1.21",
	}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveWithOptions(Memory{
		ID: saved.Memory.ID, Description: "release/1.22", Body: "release/1.22",
		SubjectKey: "project.release_branch",
	}, SaveOptions{}); err != nil {
		t.Fatalf("re-saving the holder itself must not self-conflict: %v", err)
	}
	if _, err := store.SaveWithOptions(Memory{
		Name: "unrelated-note", Description: "no subject here", Body: "narrative fact",
	}, SaveOptions{}); err != nil {
		t.Fatalf("facts without a subject key are exempt: %v", err)
	}
}
