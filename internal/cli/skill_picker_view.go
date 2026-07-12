package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/i18n"
	"reasonix/internal/skill"
)

const (
	skillDialogMinRows = 8
	skillDialogMaxRows = 18
)

func (m chatTUI) renderSkillPicker() string {
	p := m.skillPick
	if p == nil {
		return ""
	}
	w := max(viewWidth(m.width), 40)
	switch p.mode {
	case pickerSkills:
		return managerContentPanelStyle(w).Render(m.renderSkillPickerSkills())
	case pickerSources:
		return managerContentPanelStyle(w).Render(m.renderSkillPickerSources())
	case pickerSourceSkills:
		return managerContentPanelStyle(w).Render(m.renderSkillPickerSourceSkills())
	case pickerDetail:
		return managerContentPanelStyle(w).Render(m.renderSkillPickerDetail())
	case pickerConfirmDelete:
		return managerContentPanelStyle(w).Render(m.renderSkillPickerConfirmDelete())
	}
	return ""
}

func (m chatTUI) skillPickerFooterHint() string {
	if m.skillPick == nil {
		return ""
	}
	switch m.skillPick.mode {
	case pickerSkills:
		return i18n.M.SkillPickerHint
	case pickerSources:
		return i18n.M.SkillPickerSourceHint
	case pickerSourceSkills:
		return i18n.M.SkillPickerSourceSkillsHint
	case pickerDetail:
		return i18n.M.SkillPickerDetailHint
	case pickerConfirmDelete:
		return i18n.M.SkillPickerDeleteHint
	default:
		return ""
	}
}

func (m chatTUI) renderSkillPickerSkills() string {
	p := m.skillPick
	w := max(viewWidth(m.width), 40)
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", viewHeader("Manage skills"))
	if summary := skillPickerSummary(p); summary != "" {
		fmt.Fprintf(&b, "%s\n", viewMeta(summary))
	}
	b.WriteByte('\n')
	b.WriteString(renderSkillSearchBox(p.query, p.searchActive, w))
	b.WriteByte('\n')

	skills := p.skills
	if p.searchActive && p.query != "" {
		skills = p.filteredSkills()
	}

	if len(skills) == 0 {
		b.WriteString(viewMeta(i18n.M.SkillPickerSearchEmpty))
		b.WriteByte('\n')
	} else {
		start, end := skillListWindow(p.sel, len(skills), m.skillPickerVisibleRows())
		if start > 0 {
			b.WriteString(viewMeta(fmt.Sprintf(i18n.M.SkillPickerMoreAboveFmt, start)))
			b.WriteByte('\n')
		}
		lastGroup := ""
		for i := start; i < end; i++ {
			group := skillGroupLabel(skills[i].Scope)
			if group != lastGroup {
				if lastGroup != "" {
					b.WriteByte('\n')
				}
				fmt.Fprintf(&b, "  %s\n", bold(group))
				lastGroup = group
			}
			b.WriteString(renderSkillRow(i+1, i == p.sel, skills[i], p.skillEnabled(skills[i].Name), w))
			b.WriteByte('\n')
		}
		if end < len(skills) {
			b.WriteString(viewMeta(fmt.Sprintf(i18n.M.SkillPickerMoreBelowFmt, len(skills)-end)))
			b.WriteByte('\n')
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m chatTUI) skillPickerVisibleRows() int {
	if m.height <= 0 {
		return skillDialogMaxRows
	}
	return min(skillDialogMaxRows, max(skillDialogMinRows, m.height-14))
}

func skillListWindow(sel, total, limit int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if limit <= 0 || limit >= total {
		return 0, total
	}
	if sel < 0 {
		sel = 0
	}
	if sel >= total {
		sel = total - 1
	}
	start := sel - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > total {
		start = total - limit
	}
	return start, start + limit
}

func renderSkillSearchBox(query string, active bool, w int) string {
	boxWidth := max(8, w-4)
	innerWidth := max(1, boxWidth-4)
	text := "/ " + i18n.M.SkillPickerSearchPlaceholder
	if active || query != "" {
		text = "/ " + query
	}
	text = padRight(viewCompactText(text, innerWidth), innerWidth)
	var b strings.Builder
	b.WriteString(dim("  ╭" + strings.Repeat("─", boxWidth-2) + "╮"))
	b.WriteByte('\n')
	b.WriteString(dim("  │ " + text + " │"))
	b.WriteByte('\n')
	b.WriteString(dim("  ╰" + strings.Repeat("─", boxWidth-2) + "╯"))
	b.WriteByte('\n')
	return b.String()
}

func (m chatTUI) renderSkillPickerSources() string {
	p := m.skillPick
	var b strings.Builder

	b.WriteString(accent(i18n.M.SkillPickerSourceTitle))
	summary := skillSourceSummary(p.roots)
	if summary != "" {
		b.WriteString("  " + dim(summary))
	}
	b.WriteByte('\n')

	roots := p.visibleRoots()
	for i, r := range roots {
		label := sourceRowLabel(r, m.width)
		b.WriteString(rowLine(i == p.sourceSel, i+1, "", label, false))
		b.WriteByte('\n')
	}

	if p.showDiagnostics {
		b.WriteString(dim("  " + i18n.M.SkillPickerDiagShown))
	} else {
		b.WriteString(dim("  " + i18n.M.SkillPickerDiagHidden))
	}
	return b.String()
}

func (m chatTUI) renderSkillPickerSourceSkills() string {
	p := m.skillPick
	var b strings.Builder
	root, ok := p.selectedRoot()
	if !ok {
		p.mode = pickerSources
		return m.renderSkillPickerSources()
	}
	skills := p.selectedRootSkills()
	b.WriteString(accent(i18n.M.SkillPickerSourceTitle))
	b.WriteString("  " + dim(viewCompactPath(root.dir, max(8, m.width-18))))
	b.WriteByte('\n')
	b.WriteByte('\n')
	if len(skills) == 0 {
		b.WriteString(dim("  " + i18n.M.SkillPickerSourceSkillsEmpty))
		b.WriteByte('\n')
		return b.String()
	}
	start, end := skillListWindow(p.sourceSkillSel, len(skills), m.skillPickerVisibleRows())
	if start > 0 {
		b.WriteString(dim("  " + fmt.Sprintf(i18n.M.SkillPickerMoreAboveFmt, start)))
		b.WriteByte('\n')
	}
	for i := start; i < end; i++ {
		b.WriteString(renderSkillRow(i+1, i == p.sourceSkillSel, skills[i], p.skillEnabled(skills[i].Name), m.width))
		b.WriteByte('\n')
	}
	if end < len(skills) {
		b.WriteString(dim("  " + fmt.Sprintf(i18n.M.SkillPickerMoreBelowFmt, len(skills)-end)))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m chatTUI) renderSkillPickerDetail() string {
	p := m.skillPick
	var b strings.Builder
	b.WriteString(renderSkillDetailHeader(p.detailSkill, m.width))
	b.WriteByte('\n')
	b.WriteByte('\n')
	enabled := i18n.M.SkillPickerDisabledLabel
	if p.skillEnabled(p.detailSkill.Name) {
		enabled = i18n.M.SkillPickerAvailableLabel
	}
	b.WriteString(dim("  " + i18n.M.SkillPickerStatusLabel + ": " + enabled))
	b.WriteByte('\n')
	actions := skillActionsFor(p.detailSkill)
	for i, action := range actions {
		b.WriteString(rowLine(i == p.detailAction, i+1, "", action.label, false))
		b.WriteByte('\n')
	}
	if body := renderSkillBodyPreview(p.detailSkill, m.width, 6); body != "" {
		b.WriteByte('\n')
		b.WriteString(body)
	}
	return b.String()
}

func (m chatTUI) renderSkillPickerConfirmDelete() string {
	p := m.skillPick
	var b strings.Builder
	b.WriteString(accent(fmt.Sprintf(i18n.M.SkillPickerDeleteTitleFmt, p.deleteSkill.Name)))
	b.WriteByte('\n')
	path := skillDeleteTargetLabel(p.deleteSkill)
	if path != "" {
		b.WriteString(dim("  " + viewCompactPath(path, max(8, m.width-4))))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(rowLine(p.confirm == 0, 1, "", i18n.M.SkillPickerDeleteConfirm, false))
	b.WriteByte('\n')
	b.WriteString(rowLine(p.confirm == 1, 2, "", i18n.M.SkillPickerDeleteCancel, false))
	return b.String()
}

func renderSkillRow(num int, selected bool, s skill.Skill, enabled bool, w int) string {
	prefix := "    "
	if selected {
		prefix = accent("  › ")
	}
	nameWidth := min(30, max(14, w/3))
	name := compactMiddle(s.SlashName(), nameWidth)
	if selected {
		name = bold(name)
	}
	name = padRight(name, nameWidth)
	status := "✓ " + i18n.M.SkillPickerAvailableLabel
	if enabled {
		status = viewStatus(status)
	} else {
		status = viewMeta("○ " + i18n.M.SkillPickerDisabledLabel)
	}
	meta := skillRowMeta(s)
	number := fmt.Sprintf("%2d. ", num)
	line := fmt.Sprintf("%s%s%s · %s · %s", prefix, number, name, status, viewMeta(meta))
	if visibleWidth(line) > w {
		line = viewCompactText(line, w)
	}

	if selected {
		return reverse(padRight(line, w))
	}
	return line
}

func skillGroupLabel(sc skill.Scope) string {
	return titleText(scopeLabel(sc)) + " skills"
}

func skillRowMeta(s skill.Skill) string {
	parts := []string{scopeLabel(s.Scope)}
	if s.RunAs == skill.RunSubagent {
		parts = append(parts, i18n.M.SkillPickerSubagent)
	}
	parts = append(parts, fmt.Sprintf(i18n.M.SkillPickerTokenFmt, approxSkillTokens(s)))
	return strings.Join(parts, " · ")
}

func approxSkillTokens(s skill.Skill) int {
	text := strings.TrimSpace(s.Body)
	if text == "" {
		text = strings.TrimSpace(s.Description)
	}
	if text == "" {
		return 0
	}
	estimate := max(len([]rune(text))/4, len(strings.Fields(text)))
	if estimate <= 10 {
		return 10
	}
	return ((estimate + 9) / 10) * 10
}

func sourceRowLabel(r skillRootLine, w int) string {
	path := viewCompactPath(r.dir, max(8, w-40))
	scope := dim(scopeLabel(r.scope))
	status := statusLabel(r.status)
	if r.status == skill.StatusOK {
		status = accent(status)
	} else {
		status = dim(status)
	}
	skills := dim(fmt.Sprintf("%d %s", r.skills, i18n.M.SkillPickerSkillsUnit))
	return fmt.Sprintf("%s  %s  %s  %s", path, scope, status, skills)
}

func skillPickerSummary(p *skillPicker) string {
	if len(p.skills) == 0 {
		return ""
	}
	if p.searchActive && p.query != "" {
		filtered := p.filteredSkills()
		return fmt.Sprintf(i18n.M.SkillPickerMatchingFmt, len(filtered), len(p.skills))
	}
	counts := map[skill.Scope]int{}
	for _, s := range p.skills {
		counts[s.Scope]++
	}
	var parts []string
	parts = append(parts, fmt.Sprintf(i18n.M.SkillPickerAvailableFmt, len(p.skills)))
	for _, sc := range []skill.Scope{skill.ScopeProject, skill.ScopeCustom, skill.ScopeGlobal, skill.ScopeBuiltin} {
		if n, ok := counts[sc]; ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, scopeLabel(sc)))
		}
	}
	return strings.Join(parts, " · ")
}

func skillSourceSummary(roots []skillRootLine) string {
	active := 0
	for _, r := range roots {
		if r.skills > 0 {
			active++
		}
	}
	if active == 0 {
		return ""
	}
	return fmt.Sprintf(i18n.M.SkillPickerSourceActiveFmt, active)
}

func renderSkillDetail(s skill.Skill, w int) string {
	var b strings.Builder
	b.WriteString(renderSkillDetailHeader(s, w))
	if body := renderSkillBodyPreview(s, w, 12); body != "" {
		b.WriteByte('\n')
		b.WriteString(body)
	}
	return b.String()
}

func renderSkillDetailHeader(s skill.Skill, w int) string {
	var b strings.Builder
	b.WriteString(accent("/" + s.SlashName()))
	b.WriteByte('\n')

	meta := fmt.Sprintf(i18n.M.SkillPickerDetailMetaFmt, scopeLabel(s.Scope), string(s.RunAs))
	b.WriteString(dim("  " + meta))
	b.WriteByte('\n')

	if s.Path != "" && s.Scope != skill.ScopeBuiltin {
		b.WriteString(dim("  " + viewCompactPath(s.Path, max(8, w-4))))
		b.WriteByte('\n')
	}

	if strings.TrimSpace(s.Description) != "" {
		b.WriteByte('\n')
		b.WriteString(viewCompactText(s.Description, max(8, w-4)))
		b.WriteByte('\n')
	}

	return b.String()
}

func renderSkillBodyPreview(s skill.Skill, w, maxLines int) string {
	body, extra := viewBodyPreview(s.Body, maxLines)
	if body == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(dim(viewProtectLines(body, w)))
	b.WriteByte('\n')
	if extra > 0 {
		b.WriteString(viewMore(extra, i18n.M.SkillPickerLinesUnit))
		b.WriteByte('\n')
	}
	return b.String()
}

func scopeLabel(sc skill.Scope) string {
	switch sc {
	case skill.ScopeProject:
		return i18n.M.SkillPickerScopeProject
	case skill.ScopeCustom:
		return i18n.M.SkillPickerScopeCustom
	case skill.ScopeGlobal:
		return i18n.M.SkillPickerScopeGlobal
	case skill.ScopeBuiltin:
		return i18n.M.SkillPickerScopeBuiltin
	default:
		return string(sc)
	}
}

func statusLabel(st skill.PathStatus) string {
	switch st {
	case skill.StatusOK:
		return i18n.M.SkillPickerStatusOK
	case skill.StatusMissing:
		return i18n.M.SkillPickerStatusMissing
	case skill.StatusNotDirectory:
		return i18n.M.SkillPickerStatusNotDir
	case skill.StatusUnreadable:
		return i18n.M.SkillPickerStatusUnreadable
	default:
		return string(st)
	}
}
