package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAssessRememberWriteAutoAllowsOnlyLowRiskProjectCreates(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	safe := json.RawMessage(`{"name":"release-target","description":"Release target for this project","type":"project","body":"Release artifacts are published from main-v2."}`)
	assessment := AssessRememberWrite(store, safe)
	if !assessment.AutoAllow || assessment.Name != "release-target" || assessment.Reason == "" {
		t.Fatalf("safe project create assessment = %+v", assessment)
	}

	cases := map[string]json.RawMessage{
		"implicit type":    json.RawMessage(`{"name":"release-target","description":"Release target","body":"Use main-v2."}`),
		"global":           json.RawMessage(`{"name":"release-target","description":"Release target","type":"project","scope":"global","body":"Use main-v2."}`),
		"global reference": json.RawMessage(`{"name":"global/release-target.md","description":"Release target","type":"project","body":"Use main-v2."}`),
		"user preference":  json.RawMessage(`{"name":"prefers-go","description":"Preferred language","type":"user","body":"Prefer Go."}`),
		"feedback":         json.RawMessage(`{"name":"concise","description":"Response style","type":"feedback","body":"Keep answers concise."}`),
		"stable id update": json.RawMessage(`{"id":"mem-existing","expected_revision":1,"description":"Update","type":"project","body":"Updated body."}`),
		"credential":       json.RawMessage(`{"name":"deploy-key","description":"Deploy credential","type":"project","body":"DEPLOY_API_KEY=sk-example-secret-value-123456"}`),
		"email":            json.RawMessage(`{"name":"release-owner","description":"Release owner","type":"project","body":"Contact release-owner@example.test."}`),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if got := AssessRememberWrite(store, args); got.AutoAllow || got.Reason == "" {
				t.Fatalf("assessment = %+v, want approval with reason", got)
			}
		})
	}
}

func TestAssessRememberWriteRejectsConflictingReferenceScope(t *testing.T) {
	store := Store{Dir: t.TempDir(), GlobalDir: t.TempDir()}
	args := json.RawMessage(`{"name":"global/release-target.md","description":"Release target","type":"project","scope":"project","body":"Use main-v2."}`)
	got := AssessRememberWrite(store, args)
	if got.AutoAllow || !strings.Contains(got.Reason, "conflicts") {
		t.Fatalf("conflicting reference assessment = %+v", got)
	}
}

func TestAssessRememberWriteRequiresApprovalForExistingOrSemanticDuplicate(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	if _, err := store.Save(Memory{
		Name: "release-target", Title: "Release target", Description: "Current release branch",
		Type: TypeProject, Scope: FactScopeProject, Body: "Use main-v2.",
	}); err != nil {
		t.Fatal(err)
	}

	for _, args := range []json.RawMessage{
		json.RawMessage(`{"name":"release-target","description":"Changed release branch","type":"project","body":"Use release-v2."}`),
		json.RawMessage(`{"name":"another-name","title":"Release target","description":"Current release branch","type":"project","body":"Use main-v2."}`),
	} {
		if got := AssessRememberWrite(store, args); got.AutoAllow || !strings.Contains(got.Reason, "existing") {
			t.Fatalf("duplicate assessment = %+v", got)
		}
	}
}

func TestRememberAutoWriteClaimRemainsCreateOnlyAtExecution(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	args := json.RawMessage(`{"name":"release-target","description":"Release target","type":"project","body":"Use main-v2."}`)
	claim := &fakeAutoWriteQueue{claim: true}
	ctx := WithQueue(context.Background(), claim)

	// Simulate another writer creating the same name after approval assessment but
	// before the remember tool executes.
	if _, err := store.Save(Memory{Name: "release-target", Description: "concurrent", Body: "Do not overwrite."}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRememberTool(store).Execute(ctx, args); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("auto-approved create became an overwrite: %v", err)
	}
	got, ok := store.Read("release-target")
	if !ok || got.Body != "Do not overwrite." {
		t.Fatalf("concurrent memory was overwritten: %+v, ok=%v", got, ok)
	}
}

type fakeAutoWriteQueue struct {
	notes []string
	claim bool
}

func (q *fakeAutoWriteQueue) QueueMemory(note string) { q.notes = append(q.notes, note) }
func (q *fakeAutoWriteQueue) ClaimAutoMemoryWrite(json.RawMessage) bool {
	claimed := q.claim
	q.claim = false
	return claimed
}
