package memory

import (
	"encoding/json"
	"testing"

	"reasonix/internal/base/testenv"
)

// The switch widens one thing - an update to a project fact - and keeps every
// other bound, so what it lets through is still the shape the low-risk path
// trusts.
func TestAssessRememberSkipKeepsEveryOtherBound(t *testing.T) {
	store := Store{Dir: testenv.TempDir(t), GlobalDir: testenv.TempDir(t)}

	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"release-target","type":"project","description":"Release target","body":"Ship from main-v2."}`)); !got.AutoAllow {
		t.Fatalf("a bounded project fact = %+v, want allowed", got)
	}

	// A classification the low-risk path does not trust keeps asking.
	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"prefers-go","type":"user","description":"Preferred language","body":"Prefer Go."}`)); got.AutoAllow {
		t.Fatalf("a preference = %+v, want asking", got)
	}
	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"release-target","description":"Release target","body":"Ship from main-v2."}`)); got.AutoAllow {
		t.Fatalf("an unclassified fact = %+v, want asking", got)
	}

	// A global fact is not the project's to skip, addressed by name or by scope.
	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"global/release-target.md","type":"project","description":"Release target","body":"Ship from main-v2."}`)); got.AutoAllow {
		t.Fatalf("a global reference = %+v, want asking", got)
	}
	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"release-target","type":"project","scope":"global","description":"Release target","body":"Ship from main-v2."}`)); got.AutoAllow {
		t.Fatalf("an explicit global scope = %+v, want asking", got)
	}

	// Sensitive bodies keep asking whatever the switch says.
	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"deploy-key","type":"project","description":"Deploy credential","body":"DEPLOY_API_KEY=sk-example-secret-value-123456"}`)); got.AutoAllow {
		t.Fatalf("a credential = %+v, want asking", got)
	}

	// A call whose target cannot be resolved is not a project one, so it asks.
	if got := AssessRememberSkip(store, json.RawMessage(`{"id":"mem-missing","type":"project","description":"Update","body":"Updated."}`)); got.AutoAllow {
		t.Fatalf("an unresolvable target = %+v, want asking", got)
	}
}

// A fact the store already has speaks first: a call that restates a different
// scope than the stored one is the write that would move or delete it, so the
// switch stays out of it whatever the call says.
func TestAssessRememberSkipRefusesAScopeThatContradictsTheStoredFact(t *testing.T) {
	store := Store{Dir: testenv.TempDir(t), GlobalDir: testenv.TempDir(t)}
	if _, err := store.SaveWithOptions(Memory{
		Name: "keep", Scope: FactScopeGlobal, Description: "Deploy host", Body: "Deploys go to gpu-01.",
	}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}

	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"keep","type":"project","description":"Deploy host","body":"Deploys go to gpu-02."}`)); got.AutoAllow {
		t.Fatalf("a global fact addressed by name = %+v, want asking", got)
	}
	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"keep","type":"project","scope":"project","description":"Deploy host","body":"Deploys go to gpu-02."}`)); got.AutoAllow {
		t.Fatalf("a global fact restated as project = %+v, want asking", got)
	}
}

// Setting or changing activation pins a body into every later session's prefix,
// which is a different promise than a bounded fact, so it keeps asking.
func TestAssessRememberSkipRefusesPinning(t *testing.T) {
	store := Store{Dir: testenv.TempDir(t), GlobalDir: testenv.TempDir(t)}
	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"release-target","type":"project","activation":"pinned","description":"Release target","body":"Ship from main-v2."}`)); got.AutoAllow {
		t.Fatalf("a pinned fact = %+v, want asking", got)
	}
	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"release-target","type":"project","activation":"relevant","description":"Release target","body":"Ship from main-v2."}`)); got.AutoAllow {
		t.Fatalf("a restated activation = %+v, want asking", got)
	}
}

// Rewriting one memory is the switch's business; minting a near-duplicate of
// another is not, so the same shape under a different name still asks.
func TestAssessRememberSkipRefusesANearDuplicateOfAnotherFact(t *testing.T) {
	store := Store{Dir: testenv.TempDir(t), GlobalDir: testenv.TempDir(t)}
	if _, err := store.SaveWithOptions(Memory{
		Name: "release-target", Type: TypeProject, Scope: FactScopeProject,
		Description: "Project release target", Body: "Release from main-v2.",
	}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := AssessRememberSkip(store, json.RawMessage(`{"name":"release-branch","type":"project","description":"Project release target","body":"Release from main-v2."}`)); got.AutoAllow {
		t.Fatalf("a near duplicate = %+v, want asking", got)
	}
}
