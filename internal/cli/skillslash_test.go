package cli

import (
	"testing"

	"reasonix/internal/skill"
)

// TestSlashItemsExcludesSkills proves installed skills are NOT offered in the
// slash menu as "/<name>" — they are reached through "/skills " (which pops
// the same picker). Built-in verbs stay listed, and the skill data is still
// discoverable via the /skills subcommand picker.
func TestSlashItemsExcludesSkills(t *testing.T) {
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
	for _, missing := range []string{"/init", "/explore", "/superpowers:writing-plans", "/toolbox:writing-plans"} {
		if got[missing] {
			t.Errorf("slash menu must not list skill %q; have %v", missing, labels(m.slashItems()))
		}
	}
	for _, want := range []string{"/skills", "/plugins", "/hooks", "/model"} {
		if !got[want] {
			t.Errorf("slash menu missing built-in %q; have %v", want, labels(m.slashItems()))
		}
	}

	// Typing a skill's short name no longer opens a slash menu entry; the menu
	// closes because nothing matches.
	m.input.SetValue("/init")
	m.updateCompletion()
	if m.completion.active {
		t.Fatalf("typing /init must not open the slash menu: %v", labels(m.completion.items))
	}

	// The skills are still discoverable through the /skills picker: "/skills "
	// pops the picker with the management commands AND every installed skill.
	m.input.SetValue("/skills ")
	m.updateCompletion()
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatalf("/skills <space> should pop the subcommand picker: %+v", m.completion)
	}
	if !hasLabel(m.completion.items, "show") {
		t.Fatalf("/skills <space> should list the management commands: %+v", labels(m.completion.items))
	}
	found := map[string]int{}
	for i, it := range m.completion.items {
		found[it.label] = i
	}
	for _, want := range []string{"init", "explore"} {
		if _, ok := found[want]; !ok {
			t.Errorf("skill %q missing from /skills picker; have %v", want, labels(m.completion.items))
		}
	}
	// Both plugin-qualified skills share the short name "writing-plans"; the
	// picker lists them as-is (the qualified plugin names are the accept
	// targets of the /slash-name compatibility filter).
	n := 0
	for _, it := range m.completion.items {
		if it.label == "writing-plans" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("writing-plans should appear twice (superpowers + toolbox), got %d", n)
	}
	// The management commands rank before the skills.
	if showIdx, ok := found["show"]; ok {
		for _, name := range []string{"init", "explore"} {
			if found[name] < showIdx {
				t.Errorf("skill %q should follow the management commands in the picker", name)
			}
		}
	}
}
