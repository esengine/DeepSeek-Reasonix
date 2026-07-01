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
	}

	got := map[string]bool{}
	for _, it := range m.slashItems() {
		got[it.label] = true
	}
	for _, want := range []string{"/init", "/explore", "/skills", "/hooks", "/model"} {
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
}
