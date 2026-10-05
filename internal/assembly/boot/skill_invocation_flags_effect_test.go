package boot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/session/control"
	"strings"
	"testing"
	"time"
)

const (
	userOnlySkill  = "---\nname: ship-it\ndescription: deploys to production\ndisable-model-invocation: true\n---\nSHIP BODY\n"
	modelOnlySkill = "---\nname: ctx-notes\ndescription: background notes\nuser-invocable: false\n---\nCTX BODY\n"
	plainSkill     = "---\nname: plain-one\ndescription: an ordinary skill\n---\nPLAIN BODY\n"
)

func (h *projectionHarness) writeSkillFile(name, content string) {
	h.t.Helper()
	dir := filepath.Join(h.dir, ".reasonix", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func wholeRequest(t *testing.T, tools any, system string) string {
	t.Helper()
	raw, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + system
}

// A skill its author reserved for the user reaches nothing the model reads, and
// one reserved for the model reaches nothing the user types, through the real
// assembly, with the listing still riding the turn rather than the prefix.
func TestEffectSkillInvocationFlagsAtTheProviderBoundary(t *testing.T) {
	h := newProjectionHarness(t, "skillflags-effect", "", "")
	h.writeSkillFile("ship-it", userOnlySkill)
	h.writeSkillFile("ctx-notes", modelOnlySkill)
	h.writeSkillFile("plain-one", plainSkill)
	h.restart()

	first := h.turn("turn-alpha")
	listing := blockOf(projectionOf(t, first, "turn-alpha"), "available-skills")
	if strings.Contains(listing, "ship-it") {
		t.Fatalf("a disable-model-invocation skill reached the model's listing:\n%s", listing)
	}
	if !strings.Contains(listing, "ctx-notes") || !strings.Contains(listing, "plain-one") {
		t.Fatalf("model-invocable skills missing from the listing:\n%s", listing)
	}
	if surface := wholeRequest(t, first.Tools, systemOf(first)); strings.Contains(surface, "ship-it") {
		t.Fatalf("a disable-model-invocation skill reached the tool schemas or prefix")
	}

	for _, sk := range h.ctrl.SlashSkills() {
		if sk.Name == "ctx-notes" {
			t.Fatal("a user-invocable:false skill is offered on the slash surface")
		}
	}

	h.ctrl.Submit("/ship-it")
	deadline := time.Now().Add(30 * time.Second)
	for h.ctrl.Running() {
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	reqs := h.rec.requests()
	last := reqs[len(reqs)-1]
	var user string
	for _, m := range last.Messages {
		if m.Role == "user" {
			user = m.Content
		}
	}
	if !strings.Contains(user, "SHIP BODY") {
		t.Fatalf("the user's own /ship-it did not deliver the skill:\n%s", user)
	}
}

// The projection rules hold for the new flags: flipping the declaration on
// disk is canonical state the next turn must carry, an unchanged registry
// re-sends nothing, and the prefix never moves.
func TestEffectSkillInvocationFlagChangeIsProjectedOnceAndPrefixStable(t *testing.T) {
	h := newProjectionHarness(t, "skillflags-fresh", "", "")
	h.writeSkillFile("ship-it", userOnlySkill)
	h.restart()

	before := h.turn("turn-alpha")
	if strings.Contains(blockOf(projectionOf(t, before, "turn-alpha"), "available-skills"), "ship-it") {
		t.Fatal("precondition: ship-it must start hidden from the model")
	}
	if l := blockOf(projectionOf(t, h.turn("turn-steady"), "turn-steady"), "available-skills"); l != "" {
		t.Fatalf("an unchanged registry re-sent the listing:\n%s", l)
	}

	h.writeSkillFile("ship-it", strings.Replace(userOnlySkill, "disable-model-invocation: true\n", "", 1))
	after := h.turn("turn-beta")
	if !strings.Contains(blockOf(projectionOf(t, after, "turn-beta"), "available-skills"), "ship-it") {
		t.Fatal("freshness: dropping the flag on disk never reached the next turn")
	}
	if a, b := systemOf(before), systemOf(after); a != b {
		t.Fatalf("cache-boundary: a flag change moved the prefix:\nfirst diff site: %q", firstDivergence(a, b))
	}

	h.writeSkillFile("ship-it", userOnlySkill)
	again := h.turn("turn-gamma")
	if l := blockOf(projectionOf(t, again, "turn-gamma"), "available-skills"); strings.Contains(l, "ship-it") || l == "" {
		t.Fatalf("freshness: re-adding the flag must re-send a listing without ship-it, got:\n%s", l)
	}
}

// The user-side half at the provider boundary: neither a typed /name nor an
// invocation chip may deliver a user-invocable:false skill.
func TestEffectUserInvocableFalseNeverDeliversTheSkill(t *testing.T) {
	h := newProjectionHarness(t, "skillflags-userside", "", "")
	h.writeSkillFile("ctx-notes", modelOnlySkill)
	h.restart()

	wait := func() {
		deadline := time.Now().Add(30 * time.Second)
		for h.ctrl.Running() {
			if time.Now().After(deadline) {
				t.Fatal("turn did not finish")
			}
			time.Sleep(time.Millisecond)
		}
	}
	h.ctrl.Submit("/ctx-notes")
	wait()
	h.ctrl.SubmitInvocationDisplay("/ctx-notes", "", []control.InvocationRequest{{Name: "ctx-notes", Kind: "skill"}})
	wait()
	for _, req := range h.rec.requests() {
		for _, m := range req.Messages {
			if m.Role == "user" && strings.Contains(m.Content, "CTX BODY") {
				t.Fatalf("a user-invocable:false skill body reached the model through a user path:\n%s", m.Content)
			}
		}
	}
}
