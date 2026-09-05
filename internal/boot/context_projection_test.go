package boot

// Context projection at the provider boundary. Two failures, named apart
// because their fixes are opposite: cache-boundary is canonical state reaching
// the system prefix, freshness is canonical state not reaching the next turn.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

// projectionHarness drives real turns through the real Build assembly against a
// recording provider, so every assertion reads bytes that actually reached the
// boundary rather than an intermediate the host happens to expose.
type projectionHarness struct {
	t      *testing.T
	dir    string
	kind   string
	rec    *effectRecordingProvider
	events *projectionEventLog
	ctrl   *control.Controller
}

// projectionEventLog records what the host announced, so an arm can witness the
// precondition it depends on instead of assuming it. A fold that never happened
// proves nothing about what a fold re-owes.
type projectionEventLog struct {
	mu    sync.Mutex
	kinds []event.Kind
	full  []event.Event
}

func (l *projectionEventLog) Emit(e event.Event) {
	l.mu.Lock()
	l.kinds = append(l.kinds, e.Kind)
	l.full = append(l.full, e)
	l.mu.Unlock()
}

func (l *projectionEventLog) saw(kind event.Kind) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Contains(l.kinds, kind)
}

func newProjectionHarness(t *testing.T, kind, agentConfig, providerConfig string) *projectionHarness {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &effectRecordingProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
`+agentConfig+`

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`+providerConfig+`
`)
	h := &projectionHarness{t: t, dir: dir, kind: kind, rec: rec, events: &projectionEventLog{}}
	h.build()
	t.Cleanup(func() {
		if h.ctrl != nil {
			h.ctrl.Close()
		}
	})
	return h
}

func (h *projectionHarness) build() {
	h.t.Helper()
	ctrl, err := Build(context.Background(), Options{Sink: h.events})
	if err != nil {
		h.t.Fatalf("Build: %v", err)
	}
	h.ctrl = ctrl
}

// restart destroys the live session and assembles a new one over the same
// canonical state. What survives is only what the projection can rebuild — a
// freshness result that holds only inside one process is process-local state,
// not a projection.
func (h *projectionHarness) restart() {
	h.t.Helper()
	h.ctrl.Close()
	h.ctrl = nil
	h.build()
}

// turn runs one prompt and returns the first request it put on the wire. The
// cursor is taken here rather than carried between turns because the host makes
// provider calls of its own — a compaction summary is one — and counting those
// as the next turn reads the summarizer's prompt as the model's context.
func (h *projectionHarness) turn(prompt string) provider.Request {
	h.t.Helper()
	start := len(h.rec.requests())
	if err := h.ctrl.Run(context.Background(), prompt); err != nil {
		h.t.Fatalf("Run(%q): %v", prompt, err)
	}
	reqs := h.rec.requests()
	if len(reqs) <= start {
		h.t.Fatalf("Run(%q) never reached the provider", prompt)
	}
	return reqs[start]
}

// projectionOf is this turn's host-authored context: the user message carrying
// the turn's text, less that text. Not the whole history, which reports a
// listing delivered on turn one as present forever; not the last user message
// either, which under context pressure is a host notice appended after it.
func projectionOf(t *testing.T, req provider.Request, text string) string {
	t.Helper()
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, text) {
			return strings.ReplaceAll(m.Content, text, "")
		}
	}
	t.Fatalf("no user message on this request carries the turn's own text %q, so there is no projection to read", text)
	return ""
}

// Arm 1: the same canonical state, projected by two different sessions, must
// produce the same bytes — prefix and projection both. Without this row the
// rest of the matrix cannot be read: a difference elsewhere could be the
// assembly's own nondeterminism (map order, retrieval order) rather than the
// canonical change under test.
func TestProjectionSameStateProjectsIdenticallyInANewSession(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-stable", "", "")
	writeFile(t, h.dir, "REASONIX.md", "Project rule: keep the prefix stable.")
	h.restart()

	first := h.turn("turn-alpha")
	h.restart()
	second := h.turn("turn-alpha")

	if a, b := systemOf(first), systemOf(second); a != b {
		t.Fatalf("cache-boundary: two sessions over one canonical state composed different prefixes:\nfirst diff site: %q", firstDivergence(a, b))
	}
	if a, b := projectionOf(t, first, "turn-alpha"), projectionOf(t, second, "turn-alpha"); a != b {
		t.Fatalf("the projection is not a function of canonical state; it differs between sessions that share one:\nfirst diff site: %q", firstDivergence(a, b))
	}
}

// Arm 2: a memory write changes canonical state. The prefix must not move, and
// the very next request must carry the new state — in this session, and again
// in one assembled after this one is destroyed.
func TestProjectionMemoryUpdateRidesTheTurnAndSurvivesRestart(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-memory", "", "")
	before := h.turn("turn-alpha")

	const note = "Always run make lint before pushing."
	if _, err := h.ctrl.QuickAdd(memory.ScopeProject, note); err != nil {
		t.Fatalf("QuickAdd: %v", err)
	}

	after := h.turn("turn-beta")
	if !strings.Contains(projectionOf(t, after, "turn-beta"), note) {
		t.Fatalf("freshness: the instruction the host just wrote did not reach the next request:\n%s", projectionOf(t, after, "turn-beta"))
	}
	if a, b := systemOf(before), systemOf(after); a != b {
		t.Fatalf("cache-boundary: a memory write moved the prefix mid-session:\nfirst diff site: %q", firstDivergence(a, b))
	}

	h.restart()
	restarted := h.turn("turn-gamma")
	if !strings.Contains(projectionOf(t, restarted, "turn-gamma"), note) {
		t.Fatalf("freshness: the instruction did not survive the session that wrote it:\n%s", projectionOf(t, restarted, "turn-gamma"))
	}
	if a, b := systemOf(before), systemOf(restarted); a != b {
		t.Fatalf("cache-boundary: a memory write moved the next session's prefix:\nfirst diff site: %q", firstDivergence(a, b))
	}
}

const projectionSkillFile = `---
name: ledger-audit
description: Audits the evidence ledger for receipts nothing reads.
---

Read the ledger and report.
`

// Arm 3: the skill registry is canonical state the host itself writes. The same
// two questions: the prefix must not move, and the next request must project
// the registry as it now is.
func TestProjectionSkillRegistryChangeReachesTheNextTurn(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-skills", "", "")
	before := h.turn("turn-alpha")
	if !strings.Contains(projectionOf(t, before, "turn-alpha"), "<available-skills>") {
		t.Fatalf("no skill listing reached the boundary, so this arm measures nothing:\n%s", projectionOf(t, before, "turn-alpha"))
	}

	if _, err := h.ctrl.CreateSkill("ledger-audit", skill.ScopeProject, projectionSkillFile); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if found := h.ctrl.Skills(); !skillListed(found, "ledger-audit") {
		t.Fatalf("the write never reached the canonical registry this arm is about: %v", skillNames(found))
	}

	after := h.turn("turn-beta")
	if a, b := systemOf(before), systemOf(after); a != b {
		t.Fatalf("cache-boundary: a skill write moved the prefix mid-session:\nfirst diff site: %q", firstDivergence(a, b))
	}
	// Reported rather than fatal: the restart row below is a different question
	// and a session-local answer must not hide it.
	if listing := blockOf(projectionOf(t, after, "turn-beta"), "available-skills"); !strings.Contains(listing, "ledger-audit") {
		t.Errorf("freshness: the skill the host just wrote is in the canonical registry and not in the next request:\n%s", listing)
	}

	h.restart()
	restarted := h.turn("turn-gamma")
	if listing := blockOf(projectionOf(t, restarted, "turn-gamma"), "available-skills"); !strings.Contains(listing, "ledger-audit") {
		t.Errorf("freshness: a new session did not project the registry it read from disk:\n%s", listing)
	}
	if a, b := systemOf(before), systemOf(restarted); a != b {
		t.Fatalf("cache-boundary: a skill write moved the next session's prefix:\nfirst diff site: %q", firstDivergence(a, b))
	}
}

// Arm 4: a fold can summarise away the turn a once-delivered block rode on, so
// the block is owed again. What it owes is the registry as it is now — the
// point of re-owing is that the host's knowledge is live, not that the old
// bytes are replayed.
func TestProjectionCompactionReOwesTheLatestCanonicalState(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-compaction", "\ncompact_ratio = 0.4\n", "\ncontext_window = 12000\n")
	// A fold needs a history taller than the retained tail, so the arm builds
	// one. The filler is what makes the turns foldable at all: with short ones
	// every message stays verbatim and the manual compaction is a silent noop.
	filler := strings.Repeat("filler sentence about the ledger. ", 200)
	for _, prompt := range []string{"turn-1", "turn-2", "turn-3", "turn-4", "turn-5", "turn-6"} {
		h.turn(prompt + " " + filler)
	}

	if _, err := h.ctrl.CreateSkill("ledger-audit", skill.ScopeProject, projectionSkillFile); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if err := h.ctrl.Compact(context.Background(), ""); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !h.events.saw(event.CompactionDone) {
		t.Fatalf("no fold completed, so this arm measures nothing: %v", h.events.kinds)
	}

	after := h.turn("turn-eta")
	got := projectionOf(t, after, "turn-eta")
	if !strings.Contains(got, "<available-skills>") {
		t.Fatalf("freshness: the fold left the session with no listing at all:\n%s", got)
	}
	if listing := blockOf(got, "available-skills"); !strings.Contains(listing, "ledger-audit") {
		t.Fatalf("freshness: the re-owed listing is the one the fold summarised away, not the registry as it now is:\n%s", listing)
	}
}

// blockOf extracts one host block from a projection, so a failure reports the
// listing under test instead of the whole turn.
func blockOf(projection, tag string) string {
	open, close := "<"+tag+">", "</"+tag+">"
	i := strings.Index(projection, open)
	if i < 0 {
		return ""
	}
	j := strings.Index(projection[i:], close)
	if j < 0 {
		return projection[i:]
	}
	return projection[i : i+j+len(close)]
}

// Arm 6: an editor, a script and the model's own write tool all reach the
// registry, and the listing is owed by asking it rather than by being told, so
// a write nobody announced still lands on the next turn. The second half reads
// the rule backwards: a turn that changed nothing must not re-send the listing,
// or freshness is bought by destroying the stability it rides on.
func TestProjectionOutOfBandSkillWriteReachesTheNextTurn(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-outofband", "", "")
	before := h.turn("turn-alpha")

	dir := filepath.Join(h.dir, ".reasonix", "skills", "outside-audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make the skill directory: %v", err)
	}
	file := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(file, []byte(strings.ReplaceAll(projectionSkillFile, "ledger-audit", "outside-audit")), 0o644); err != nil {
		t.Fatalf("write the skill past every API: %v", err)
	}

	after := h.turn("turn-beta")
	if listing := blockOf(projectionOf(t, after, "turn-beta"), "available-skills"); !strings.Contains(listing, "outside-audit") {
		t.Errorf("freshness: a skill written past the host's writers never reached the model:\n%s", listing)
	}
	if a, b := systemOf(before), systemOf(after); a != b {
		t.Errorf("cache-boundary: an out-of-band skill write moved the prefix:\nfirst diff site: %q", firstDivergence(a, b))
	}

	if listing := blockOf(projectionOf(t, h.turn("turn-gamma"), "turn-gamma"), "available-skills"); listing != "" {
		t.Errorf("the listing was re-sent although the registry did not change:\n%s", listing)
	}

	// A touch is the case a metadata fingerprint gets wrong: the file moved,
	// the line the model reads did not.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatalf("touch the skill file: %v", err)
	}
	if listing := blockOf(projectionOf(t, h.turn("turn-delta"), "turn-delta"), "available-skills"); listing != "" {
		t.Errorf("a touched mtime re-sent a listing that did not change:\n%s", listing)
	}
}

// Arm 7: the durable switch is canonical registry state, so it answers in the
// session that flipped it. The rows are the toggle in both directions, the
// listing owed exactly once for each, the negative control that an unchanged
// registry re-sends nothing, and a rebuild landing on the same bytes the live
// session ended with.
func TestProjectionActivationChangeReachesTheLiveSession(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-activation", "", "")
	// The entry line, never the bare name: the index preamble names a built-in
	// as its own call example, so matching the name matches the boilerplate.
	const entry = "\n- explore "
	before := h.turn("turn-alpha")
	if listing := blockOf(projectionOf(t, before, "turn-alpha"), "available-skills"); !strings.Contains(listing, entry) {
		t.Fatalf("the skill this arm toggles is not in the listing to begin with:\n%s", listing)
	}

	// A toggle that changes nothing writes a durable row and still owes nothing:
	// the debt is against the listing, not against the file that decides it.
	if err := h.ctrl.SetSkillEnabled("explore", config.ActivationProject, true); err != nil {
		t.Fatalf("SetSkillEnabled(no-op): %v", err)
	}
	if listing := blockOf(projectionOf(t, h.turn("turn-beta"), "turn-beta"), "available-skills"); listing != "" {
		t.Errorf("a toggle that changed no listing re-sent one:\n%s", listing)
	}

	if err := h.ctrl.SetSkillEnabled("explore", config.ActivationProject, false); err != nil {
		t.Fatalf("SetSkillEnabled(off): %v", err)
	}
	off := blockOf(projectionOf(t, h.turn("turn-beta2"), "turn-beta2"), "available-skills")
	if off == "" {
		t.Fatalf("the switch changed the registry and the turn owed no listing")
	}
	if strings.Contains(off, entry) {
		t.Errorf("freshness: a disabled skill is still listed to the model:\n%s", off)
	}

	if again := blockOf(projectionOf(t, h.turn("turn-gamma"), "turn-gamma"), "available-skills"); again != "" {
		t.Errorf("the listing was re-sent although nothing changed after the toggle:\n%s", again)
	}

	if err := h.ctrl.SetSkillEnabled("explore", config.ActivationProject, true); err != nil {
		t.Fatalf("SetSkillEnabled(on): %v", err)
	}
	on := blockOf(projectionOf(t, h.turn("turn-delta"), "turn-delta"), "available-skills")
	if !strings.Contains(on, entry) {
		t.Errorf("freshness: re-enabling did not put the skill back in the listing:\n%s", on)
	}

	h.restart()
	rebuilt := blockOf(projectionOf(t, h.turn("turn-epsilon"), "turn-epsilon"), "available-skills")
	if rebuilt != on {
		t.Errorf("a rebuilt session does not agree with the live one it replaced:\nfirst diff site: %q", firstDivergence(on, rebuilt))
	}
	if a, b := systemOf(before), systemOf(h.turn("turn-zeta")); a != b {
		t.Errorf("cache-boundary: toggling a skill moved the prefix:\nfirst diff site: %q", firstDivergence(a, b))
	}
}

// Arm 5 is the declared exception, stated so that changing it is a decision
// rather than drift: a pinned fact's body folds into the prefix, which costs
// one cold start and is what pinning means. An ordinary saved fact must not —
// TestEffectRememberDoesNotMoveTheCachedPrefix holds that half — and no write
// at all may rewrite the prefix a live session is already sampling against.
func TestProjectionPinningIsTheDeclaredPrefixException(t *testing.T) {
	h := newProjectionHarness(t, "ctxproj-pinned", "", "")
	before := systemOf(h.turn("turn-alpha"))

	const marker = "Answer in Portuguese unless asked otherwise."
	if _, err := h.ctrl.SaveMemory(memory.Memory{
		Name: "pinned-rule", Description: "a pinned preference", Type: memory.TypeUser,
		Scope: memory.FactScopeGlobal, Activation: memory.ActivationPinned, Body: marker,
	}); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	if a, b := before, systemOf(h.turn("turn-beta")); a != b {
		t.Fatalf("cache-boundary: a pin rewrote the prefix of the session that made it:\nfirst diff site: %q", firstDivergence(a, b))
	}

	h.restart()
	after := systemOf(h.turn("turn-gamma"))
	if !strings.Contains(after, marker) {
		t.Fatalf("a pinned body did not reach the next session's prefix, which is what pinning means:\n%s", after)
	}
	if before == after {
		t.Fatal("the pinned body is in the prefix and the prefix did not move; one of the two readings is wrong")
	}
}

func skillNames(skills []skill.Skill) []string {
	out := make([]string, 0, len(skills))
	for _, sk := range skills {
		out = append(out, sk.Name)
	}
	return out
}

func skillListed(skills []skill.Skill, name string) bool {
	for _, sk := range skills {
		if sk.Name == name {
			return true
		}
	}
	return false
}
