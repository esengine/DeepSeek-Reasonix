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

// Near-duplicate phrases (containment or overlapping CJK bigrams) must be
// recognized so automatic writes do not pile up the same claim repeatedly.
func TestPhraseOverlapsNearDuplicates(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"包管理", "包管理器", true},                            // containment
		{"数据库迁移", "数据库迁移方案", true},                       // containment
		{"prefer tabs", "prefer tabs over spaces", true}, // containment
		{"pnpm", "PNPM", true},                           // exact after normalize
		{"包管理工具", "包管理器", true},                          // two shared bigrams 包管/管理
	}
	for _, tc := range cases {
		if got := phraseOverlaps(tc.a, tc.b); got != tc.want {
			t.Errorf("phraseOverlaps(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// Distinct concepts sharing only a generic bigram must not be suppressed.
func TestPhraseOverlapsDistinguishesConcepts(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"包管理", "依赖管理", false}, // only shared bigram is 管理
		{"包管理", "分支管理", false}, // only shared bigram is 管理
		{"数据库迁移", "代码重构", false},
		{"package manager", "branch management", false},
	}
	for _, tc := range cases {
		if got := phraseOverlaps(tc.a, tc.b); got != tc.want {
			t.Errorf("phraseOverlaps(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// A generic short word must not absorb an unrelated longer description.
func TestPhraseOverlapsIgnoresGenericShortWords(t *testing.T) {
	if phraseOverlaps("ok", "the project uses tabs for indentation") {
		t.Error("short generic word absorbed an unrelated description")
	}
	if phraseOverlaps("x", "switch the default branch to main") {
		t.Error("single letter absorbed an unrelated description")
	}
	// 2-char CJK words are short too: "内存" must not absorb
	// "内存泄漏定位" via containment. Byte-length guards would let this
	// through (6 bytes); the rune-count guard must not.
	if phraseOverlaps("内存", "内存泄漏定位") {
		t.Error("2-char CJK word absorbed an unrelated longer description")
	}
}

// A shared CJK domain prefix must not suppress a distinct fact: the differing
// suffix carries the meaning ("迁移" vs "备份"). Previously a 50% token
// overlap threshold flagged these as duplicates. Note the boundary: a longer
// shared core ("缓存命中率", 4 of 6 bigrams) still dedupes at two-thirds.
func TestPhraseOverlapsDistinguishesSharedPrefixSuffix(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"数据库迁移", "数据库备份", false},   // 2/4 bigrams — shared prefix only
		{"数据迁移方案", "数据备份方案", false}, // 1/3 bigrams — generic core
	}
	for _, tc := range cases {
		if got := phraseOverlaps(tc.a, tc.b); got != tc.want {
			t.Errorf("phraseOverlaps(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// Two-thirds of distinct tokens is the dedupe boundary: a shared core with a
// different tail is the same claim, in both CJK bigrams and English words.
func TestPhraseOverlapsDedupesSharedCore(t *testing.T) {
	cases := []struct {
		a, b string
	}{
		{"缓存命中率优化", "缓存命中率监控"},                             // 4/6 bigrams shared
		{"prompt cache tuning", "prompt cache monitoring"}, // 2/3 words shared
	}
	for _, tc := range cases {
		if !phraseOverlaps(tc.a, tc.b) {
			t.Errorf("phraseOverlaps(%q, %q) = false, want true (2/3 shared core)", tc.a, tc.b)
		}
	}
}

// Repeated tokens must not inflate the overlap ratio: "包管理" vs "管理管理"
// share the bigram 管理 but are different claims.
func TestPhraseOverlapsIgnoresMultiplicity(t *testing.T) {
	if phraseOverlaps("包管理", "管理管理") {
		t.Error("repeated token inflated the overlap ratio")
	}
}

// Coarse-grained English words still dedupe at the two-thirds ratio: three
// tokens sharing two is the same claim with a different tail.
func TestPhraseOverlapsDedupesEnglishTail(t *testing.T) {
	if !phraseOverlaps("uses database migrations", "uses database backups") {
		t.Error("two of three shared English tokens should dedupe")
	}
}
