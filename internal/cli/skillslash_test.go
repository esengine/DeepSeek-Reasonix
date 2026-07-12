package cli

import (
	"testing"

	"reasonix/internal/skill"
)

// TestSlashItemsIncludesSkills proves every loaded skill is offered in the slash
// menu as "/<name>" (so /init, /explore, … show up), and that typing the prefix
// filters to it — the data path behind "type / to see the commands".
func TestSlashItemsIncludesSkills(t *testing.T) {
	m := newTestChatTUI()
	m.skills = []skill.Skill{
		{Name: "init", Description: "bootstrap AGENTS.md", RunAs: skill.RunInline},
		{Name: "explore", Description: "investigate", RunAs: skill.RunSubagent},
		{Name: "writing-plans", Plugin: "superpowers", Description: "write a plan", RunAs: skill.RunInline},
		{Name: "writing-plans", Plugin: "toolbox", Description: "write another plan", RunAs: skill.RunInline},
	}

	got := map[string]bool{}
	for _, it := range m.slashItems() {
		got[it.label] = true
	}
	for _, want := range []string{"/init", "/explore", "/superpowers:writing-plans", "/toolbox:writing-plans", "/skills", "/plugins", "/hooks", "/model"} {
		if !got[want] {
			t.Errorf("slash menu missing %q; have %v", want, labels(m.slashItems()))
		}
	}

	// Typing "/init" filters the menu down to the init skill.
	m.input.SetValue("/init")
	m.updateCompletion()
	if !m.completion.active {
		t.Fatal("typing /init should open the slash menu")
	}
	found := false
	for _, it := range m.completion.items {
		if it.label == "/init" {
			found = true
		}
	}
	if !found {
		t.Errorf("/init not in filtered menu: %v", labels(m.completion.items))
	}

	// Typing the hidden short compatibility name still discovers every plugin's
	// visible qualified name, so an ambiguous short name becomes a chooser.
	m.input.SetValue("/writing-plans")
	m.updateCompletion()
	if !m.completion.active {
		t.Fatal("typing /writing-plans should open the slash menu")
	}
	filtered := map[string]int{}
	for i, it := range m.completion.items {
		filtered[it.label] = i
	}
	for _, want := range []string{"/superpowers:writing-plans", "/toolbox:writing-plans"} {
		if _, ok := filtered[want]; !ok {
			t.Errorf("short skill query missing %q; have %v", want, labels(m.completion.items))
		}
	}
	if idx, ok := filtered["/superpowers:writing-plans"]; ok {
		m.completion.sel = idx
		m.acceptCompletion()
		if got := m.input.Value(); got != "/superpowers:writing-plans " {
			t.Errorf("accept should fill the qualified skill name, got %q", got)
		}
	}
}
