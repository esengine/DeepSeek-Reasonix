package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

const (
	gatedGoSkill  = "---\nname: gated-go\ndescription: GOGATE-DESC go work\npaths: \"src/**/*.go\"\n---\nGATED GO BODY\n"
	gatedMdSkill  = "---\nname: gated-md\ndescription: MDGATE-DESC docs work\npaths: \"docs/*.md\"\n---\nGATED MD BODY\n"
	brokenSkill   = "---\nname: gated-broken\ndescription: BROKEN-DESC typo\npaths: \"!src/**\"\n---\nBROKEN BODY\n"
	ungatedSkill  = "---\nname: ungated\ndescription: UNGATED-DESC always\n---\nUNGATED BODY\n"
	gatedSkillSet = "gated-go,gated-md,gated-broken,ungated"
)

func buildGatedSkillController(t *testing.T, kind string, calls []scriptedCall) (*control.Controller, *scriptedCallProvider, string) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	p := &scriptedCallProvider{calls: calls}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	for name, body := range map[string]string{"gated-go": gatedGoSkill, "gated-md": gatedMdSkill, "gated-broken": brokenSkill, "ungated": ungatedSkill} {
		writeFile(t, dir, ".reasonix/skills/"+name+"/SKILL.md", body)
	}
	writeFile(t, dir, "src/a.go", "package src\n")
	writeFile(t, dir, "docs/readme.md", "# hi\n")
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	return ctrl, p, dir
}

// requestText is everything the provider was sent in one request, which is the
// only place "the model saw it" can be answered.
func requestText(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content + "\n")
	}
	return b.String()
}

func (p *scriptedCallProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

// lastUserMessage is the newest user turn of the newest request: where a fresh
// listing rides, since older listings stay in history as they were sent.
func lastUserMessage(p *scriptedCallProvider) string {
	reqs := p.requests()
	msgs := reqs[len(reqs)-1].Messages
	for _, m := range slices.Backward(msgs) {
		if m.Role == provider.RoleUser {
			return m.Content
		}
	}
	return ""
}

func lastRequestText(p *scriptedCallProvider) string {
	reqs := p.requests()
	return requestText(reqs[len(reqs)-1])
}

func assertListing(t *testing.T, label, text string, want, absent []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s: the request lacks %q", label, w)
		}
	}
	for _, a := range absent {
		if strings.Contains(text, a) {
			t.Errorf("%s: the request carries %q", label, a)
		}
	}
}

// A gated skill is absent from every request until a file it names is touched,
// present on the next turn after, and stays that way for a resumed session.
func TestEffectGatedSkillEntersTheListingOnlyAfterItsPathIsTouched(t *testing.T) {
	ctrl, p, dir := buildGatedSkillController(t, "gated-listing", []scriptedCall{
		{"read_file", `{"path":"src/a.go"}`},
	})
	if err := ctrl.Run(context.Background(), "first turn"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, req := range p.requests() {
		assertListing(t, fmt.Sprintf("turn 1 round %d", i), requestText(req),
			[]string{"UNGATED-DESC"}, []string{"GOGATE-DESC", "MDGATE-DESC", "BROKEN-DESC"})
	}
	turn1 := p.requests()
	saved := sessionstore.NewSession("BASE")
	for _, m := range ctrl.History() {
		saved.Add(m)
	}
	if err := ctrl.Run(context.Background(), "second turn"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertListing(t, "turn 2", lastUserMessage(p),
		[]string{"UNGATED-DESC", "GOGATE-DESC"}, []string{"MDGATE-DESC", "BROKEN-DESC"})

	assertStablePrefix(t, turn1[len(turn1)-1], p.requests()[len(p.requests())-1])

	fresh, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer fresh.Close()
	if err := fresh.Resume(saved, filepath.Join(dir, "resumed.jsonl")); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := fresh.Run(context.Background(), "after a restart"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertListing(t, "resumed", lastUserMessage(p),
		[]string{"UNGATED-DESC", "GOGATE-DESC"}, []string{"MDGATE-DESC", "BROKEN-DESC"})
}

// Every directory the model can read agrees with the listing, and the user's
// own /<name> never depends on a hit.
func TestEffectGatedSkillIsHiddenFromEveryModelDirectoryUntilItsPathIsTouched(t *testing.T) {
	cap := func(args string) scriptedCall { return scriptedCall{"use_capability", args} }
	calls := []scriptedCall{
		cap(`{"action":"list"}`),
		cap(`{"action":"inspect","capability_id":"skill:gated-go"}`),
		{"slash_command", `{"command":"list"}`},
		{"run_skill", `{"name":"no-such-skill"}`},
		{"read_file", `{"path":"src/a.go"}`},
		cap(`{"action":"list"}`),
		cap(`{"action":"inspect","capability_id":"skill:gated-go"}`),
		{"slash_command", `{"command":"list"}`},
		{"run_skill", `{"name":"no-such-skill"}`},
		{"run_skill", `{"name":"gated-md"}`},
	}
	ctrl, p, _ := buildGatedSkillController(t, "gated-directories", calls)
	if err := ctrl.Run(context.Background(), "survey"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, i := range []int{0, 2, 3} {
		assertListing(t, fmt.Sprintf("call %d before", i), p.resultOf(i),
			nil, []string{"gated-go", "gated-md", "gated-broken"})
	}
	if res := p.resultOf(1); strings.Contains(res, "GATED GO BODY") {
		t.Errorf("inspect of a gated skill before a hit leaked it:\n%s", res)
	}
	assertListing(t, "capability list after", p.resultOf(5), []string{"gated-go"}, []string{"gated-md", "gated-broken"})
	assertListing(t, "slash list after", p.resultOf(7), []string{"gated-go"}, []string{"gated-md", "gated-broken"})
	assertListing(t, "unknown-skill hint after", p.resultOf(8), []string{"gated-go"}, []string{"gated-md", "gated-broken"})
	assertListing(t, "run_skill by exact name, never matched", p.resultOf(9), []string{"GATED MD BODY"}, nil)

}

// The user's own /<name> never waits for a hit: the skill is on their slash
// list and its body reaches the model when they type it.
func TestEffectExplicitSlashInvocationIgnoresPathGating(t *testing.T) {
	ctrl, _, _ := buildGatedSkillController(t, "gated-slash", nil)
	var names []string
	for _, sk := range ctrl.SlashSkills() {
		names = append(names, sk.Name)
	}
	for want := range strings.SplitSeq(gatedSkillSet, ",") {
		if !slices.Contains(names, want) {
			t.Errorf("the user's slash list lacks %q before any hit: %q", want, names)
		}
	}
	sent, found := ctrl.RunSkill("/gated-md please")
	if !found || !strings.Contains(sent, "GATED MD BODY") {
		t.Fatalf("explicit /gated-md before any hit: found=%v sent=%q", found, sent)
	}
}

// A sub-agent's reads are not the session's: delegating does not make a gated
// skill appear in the delegating session's listing.
func TestEffectSubagentTouchDoesNotListAGatedSkillInTheParent(t *testing.T) {
	ctrl, p, _ := buildGatedSkillController(t, "gated-subagent", []scriptedCall{
		{"task", `{"prompt":"read src/a.go and report"}`},
		{"read_file", `{"path":"src/a.go"}`},
	})
	if err := ctrl.Run(context.Background(), "delegate"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := ctrl.Run(context.Background(), "next"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertListing(t, "after delegating", lastRequestText(p), []string{"UNGATED-DESC"}, []string{"GOGATE-DESC"})
}

// Switching a gated skill off, and rotating to a fresh conversation, both take
// it out of the next listing; switching it back on after the hit restores it.
func TestEffectGatedSkillFollowsSwitchesAndSessionRotation(t *testing.T) {
	ctrl, p, dir := buildGatedSkillController(t, "gated-neighbours", []scriptedCall{
		{"read_file", `{"path":"src/a.go"}`},
	})
	run := func(label, prompt string, want, absent []string) {
		t.Helper()
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("%s: Run: %v", label, err)
		}
		assertListing(t, label, lastUserMessage(p), want, absent)
	}
	run("hit", "touch it", nil, []string{"GOGATE-DESC"})
	run("listed", "again", []string{"GOGATE-DESC", "UNGATED-DESC"}, nil)
	if err := ctrl.SetSkillEnabled("gated-go", config.ActivationProject, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	run("switched off", "after off", []string{"UNGATED-DESC"}, []string{"GOGATE-DESC"})
	if err := ctrl.SetSkillEnabled("gated-go", config.ActivationProject, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	run("switched on", "after on", []string{"GOGATE-DESC"}, nil)
	if err := ctrl.Resume(sessionstore.NewSession("BASE"), filepath.Join(dir, "other.jsonl")); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	run("fresh conversation", "new session", []string{"UNGATED-DESC"}, []string{"GOGATE-DESC"})
}

// The listing rides the turn tail: the system message, the tool schemas and the
// history already sent are byte-identical before and after a match, and the
// new block appears only in the newest user message.
func assertStablePrefix(t *testing.T, before, after provider.Request) {
	t.Helper()
	if before.Messages[0].Role != provider.RoleSystem || after.Messages[0].Role != provider.RoleSystem {
		t.Fatal("the first message is not the system message")
	}
	if before.Messages[0].Content != after.Messages[0].Content {
		t.Error("the system message changed when a gated skill became eligible")
	}
	bt, _ := json.Marshal(before.Tools)
	at, _ := json.Marshal(after.Tools)
	if !bytes.Equal(bt, at) {
		t.Error("the tool schemas changed when a gated skill became eligible")
	}
	for i, m := range before.Messages {
		if i >= len(after.Messages) || after.Messages[i].Role != m.Role || after.Messages[i].Content != m.Content {
			t.Fatalf("history message %d changed between the two requests", i)
		}
	}
	for i, m := range after.Messages[:len(after.Messages)-1] {
		if strings.Contains(m.Content, "GOGATE-DESC") {
			t.Errorf("the new listing leaked into message %d, not the newest turn", i)
		}
	}
}
