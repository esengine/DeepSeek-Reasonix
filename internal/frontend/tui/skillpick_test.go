package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func pressSpace(m *model) { m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) }

// /skills opens the panel grouped project-first; Space only marks a pending
// switch, and Enter sends exactly the switches that changed.
func TestSkillsPanelTogglesAndSaves(t *testing.T) {
	m, k := testModel(t)
	enterSkillsLine(m, "/skills")
	if m.skills == nil || m.skills.all[0].Name != "deploy" || m.skills.all[2].Name != "test" {
		t.Fatalf("skills = %+v", m.skills)
	}
	pressSpace(m)
	run(m, press(m, "down"))
	pressSpace(m)
	if strings.Contains(strings.Join(k.seen(), "\n"), "POST /skills/enabled") {
		t.Fatal("a toggle was saved before Enter")
	}
	if v := m.View().Content; !strings.Contains(v, "[x] deploy") || !strings.Contains(v, "[ ] audit") {
		t.Fatalf("panel:\n%s", v)
	}
	run(m, press(m, "enter"))
	calls := strings.Join(k.seen(), "\n")
	for _, want := range []string{
		`POST /skills/enabled {"enabled":true,"name":"deploy","scope":"project"}`,
		`POST /skills/enabled {"enabled":false,"name":"audit","scope":"project"}`,
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("missing %s in:\n%s", want, calls)
		}
	}
	if strings.Count(calls, "POST /skills/enabled") != 2 || m.skills != nil {
		t.Fatalf("saved the unchanged skill or left the panel open:\n%s", calls)
	}
}

func TestSkillsPanelEscapeDropsPendingSwitches(t *testing.T) {
	m, k := testModel(t)
	enterSkillsLine(m, "/skills")
	pressSpace(m)
	run(m, press(m, "esc"))
	if m.skills != nil || strings.Contains(strings.Join(k.seen(), "\n"), "POST /skills/enabled") {
		t.Fatal("esc saved or kept the panel")
	}
}

// '/' starts a search, so letters stay commands (s) until then; s cycles the
// source filter.
func TestSkillsPanelSearchAndSourceFilter(t *testing.T) {
	m, _ := testModel(t)
	enterSkillsLine(m, "/skills")
	typeText(m, "s")
	if got := m.skills.shown(); len(got) != 1 || got[0].Scope != "project" {
		t.Fatalf("source filter = %+v", got)
	}
	typeText(m, "s")
	typeText(m, "s")
	typeText(m, "s")
	if m.skills.source != "" {
		t.Fatalf("filter did not cycle back to all: %q", m.skills.source)
	}
	typeText(m, "/")
	typeText(m, "tes")
	if got := m.skills.shown(); len(got) != 1 || got[0].Name != "test" {
		t.Fatalf("search = %+v", got)
	}
}

func enterSkillsLine(m *model, line string) {
	m.composer.SetValue(line)
	run(m, press(m, "enter"))
}

func TestSkillsPanelKeepsSameNamedSkillsApart(t *testing.T) {
	m, _ := testModel(t)
	run(m, func() tea.Msg {
		return skillsMsg{list: []SkillEntry{
			{Name: "lint", Scope: "project", Enabled: true},
			{Name: "lint", Scope: "global", Enabled: true},
		}}
	})
	pressSpace(m)
	if on, off := m.skills.changes(); len(on) != 0 || len(off) != 1 {
		t.Fatalf("toggling one lint moved both: on=%v off=%v", on, off)
	}
}
