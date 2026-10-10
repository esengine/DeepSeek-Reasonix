package tui

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

// SkillEntry is one discoverable skill as the kernel lists it, disabled ones
// included.
type SkillEntry struct {
	Name     string `json:"name"`
	Scope    string `json:"scope"`
	Plugin   string `json:"plugin"`
	Subagent bool   `json:"subagent"`
	Enabled  bool   `json:"enabled"`
}

func (c *Client) Skills(ctx context.Context) ([]SkillEntry, error) {
	var out struct {
		Skills []SkillEntry `json:"skills"`
	}
	err := c.do(ctx, http.MethodGet, "/skills", nil, &out)
	return out.Skills, err
}

// SetSkillEnabled switches one skill for this project.
func (c *Client) SetSkillEnabled(ctx context.Context, name string, enabled bool) error {
	return c.do(ctx, http.MethodPost, "/skills/enabled",
		map[string]any{"name": name, "enabled": enabled, "scope": "project"}, nil)
}

// key identifies a skill by scope as well as name: two scopes may each hold
// one of the same name, and a switch on one must not move the other.
func (s SkillEntry) key() string { return s.Scope + "\x00" + s.Name }

// skillScopeOrder is how the panel groups skills: what the project owns first,
// what ships in the box last.
var skillScopeOrder = []string{"project", "custom", "global", "plugin", "builtin"}

// skillPicker lists every skill with its switch. Toggles stay pending until
// Enter saves them; Esc drops them.
type skillPicker struct {
	all       []SkillEntry
	want      map[string]bool
	query     string
	searching bool
	source    string
	sel       int
}

type (
	skillsMsg struct {
		list []SkillEntry
		err  error
	}
	skillsSavedMsg struct {
		on, off []string
		err     error
	}
)

func (m *model) openSkills() tea.Cmd {
	return func() tea.Msg {
		list, err := m.client.Skills(m.ctx)
		return skillsMsg{list: list, err: err}
	}
}

func (m *model) onSkills(msg skillsMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", "skills: "+msg.err.Error())
		return m.commit()
	}
	if len(msg.list) == 0 {
		m.tr.AddNotice("info", i18n.M.ListSkillsNone)
		return m.commit()
	}
	p := &skillPicker{all: slices.Clone(msg.list), want: map[string]bool{}}
	rank := func(s string) int {
		if i := slices.Index(skillScopeOrder, s); i >= 0 {
			return i
		}
		return len(skillScopeOrder)
	}
	slices.SortStableFunc(p.all, func(a, b SkillEntry) int {
		if d := rank(a.Scope) - rank(b.Scope); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	for _, s := range p.all {
		p.want[s.key()] = s.Enabled
	}
	m.skills = p
	return nil
}

func (p *skillPicker) shown() []SkillEntry {
	q := strings.ToLower(strings.TrimSpace(p.query))
	var out []SkillEntry
	for _, s := range p.all {
		if p.source != "" && s.Scope != p.source {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(s.Name+" "+s.Scope+" "+s.Plugin), q) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// nextSource cycles the scope filter: all, then each scope the list has.
func (p *skillPicker) nextSource() {
	var scopes []string
	for _, s := range p.all {
		if !slices.Contains(scopes, s.Scope) {
			scopes = append(scopes, s.Scope)
		}
	}
	i := slices.Index(scopes, p.source)
	switch {
	case p.source == "" && len(scopes) > 0:
		p.source = scopes[0]
	case i >= 0 && i+1 < len(scopes):
		p.source = scopes[i+1]
	default:
		p.source = ""
	}
	p.sel = 0
}

func (p *skillPicker) changes() (on, off []string) {
	for _, s := range p.all {
		switch {
		case p.want[s.key()] && !s.Enabled:
			on = append(on, s.Name)
		case !p.want[s.key()] && s.Enabled:
			off = append(off, s.Name)
		}
	}
	return on, off
}

func (m *model) skillsKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.skills
	if p == nil {
		return nil, false
	}
	items := p.shown()
	k := msg.String()
	if p.searching {
		switch k {
		case "esc":
			p.searching, p.query, p.sel = false, "", 0
		case "enter":
			p.searching = false
		case "backspace":
			if p.query != "" {
				_, n := utf8.DecodeLastRuneInString(p.query)
				p.query, p.sel = p.query[:len(p.query)-n], 0
			}
		default:
			if t := typedText(msg); t != "" && !hasControl(t) {
				p.query, p.sel = p.query+t, 0
			}
		}
		return nil, true
	}
	switch k {
	case "esc", "ctrl+c":
		m.skills = nil
	case "up", "k":
		p.sel = max(p.sel-1, 0)
	case "down", "j":
		p.sel = min(p.sel+1, max(len(items)-1, 0))
	case "space":
		if p.sel < len(items) {
			k := items[p.sel].key()
			p.want[k] = !p.want[k]
		}
	case "/":
		p.searching = true
	case "s":
		p.nextSource()
	case "enter":
		on, off := p.changes()
		m.skills = nil
		if len(on)+len(off) == 0 {
			return nil, true
		}
		return func() tea.Msg { return m.saveSkills(on, off) }, true
	}
	return nil, true
}

func (m *model) saveSkills(on, off []string) tea.Msg {
	var firstErr error
	for _, set := range []struct {
		names   []string
		enabled bool
	}{{on, true}, {off, false}} {
		for _, name := range set.names {
			if err := m.client.SetSkillEnabled(m.ctx, name, set.enabled); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return skillsSavedMsg{on: on, off: off, err: firstErr}
}

func (m *model) onSkillsSaved(msg skillsSavedMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", "skills: "+msg.err.Error())
	}
	if len(msg.on)+len(msg.off) > 0 {
		m.tr.AddNotice("info", fmt.Sprintf(i18n.M.SkillPickSavedFmt, len(msg.on), len(msg.off)))
	}
	return m.commit()
}

func (m *model) skillsPanel() []string {
	p := m.skills
	items := p.shown()
	p.sel = min(p.sel, max(len(items)-1, 0))
	enabled := 0
	for _, s := range p.all {
		if p.want[s.key()] {
			enabled++
		}
	}
	lines := []string{termrender.Accent(i18n.M.SkillPickTitle),
		termrender.Dim("  " + fmt.Sprintf(i18n.M.SkillPickSummaryFmt, len(p.all), enabled))}
	if p.source != "" {
		lines = append(lines, "  "+termrender.Dim(i18n.M.SkillPickSource)+p.source)
	}
	if p.searching || p.query != "" {
		lines = append(lines, "  "+termrender.Dim(i18n.M.ResumePickSearch)+p.query)
	}
	if len(items) == 0 {
		lines = append(lines, termrender.Dim("  "+i18n.M.ResumePickNoMatch))
	}
	start := max(min(p.sel-pickerRows/2, len(items)-pickerRows), 0)
	end := min(start+pickerRows, len(items))
	if start > 0 {
		lines = append(lines, termrender.Dim("  ↑ more"))
	}
	width := max(m.width-8, 12)
	for i := start; i < end; i++ {
		s := items[i]
		box := "[ ] "
		if p.want[s.key()] {
			box = "[x] "
		}
		meta := s.Scope
		if s.Plugin != "" {
			meta += " · " + s.Plugin
		}
		if s.Subagent {
			meta += " · subagent"
		}
		label := clipVisible(s.Name+" · "+meta, width)
		lines = append(lines, rowLine(i == p.sel, i+1, box, label, p.want[s.key()] != s.Enabled))
	}
	if end < len(items) {
		lines = append(lines, termrender.Dim("  ↓ more"))
	}
	lines = append(lines, termrender.Dim(i18n.M.SkillPickHint))
	return panel(lines, m.width, accentEdge)
}
