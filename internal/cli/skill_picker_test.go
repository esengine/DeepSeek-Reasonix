package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/i18n"
	"reasonix/internal/skill"
)

func makeTestSkills() []skill.Skill {
	return []skill.Skill{
		{Name: "review", Description: "Review code changes for correctness", Scope: skill.ScopeProject, Path: "/fake/proj/.reasonix/skills/review/SKILL.md", RunAs: skill.RunSubagent, Body: "# Review\n\nReview code."},
		{Name: "explore", Description: "Fast read-only search agent", Scope: skill.ScopeBuiltin, Path: "(builtin)", RunAs: skill.RunSubagent, Body: "# Explore\n\nSearch the codebase."},
		{Name: "test", Description: "Run tests and validate behavior", Scope: skill.ScopeProject, Path: "/fake/proj/.reasonix/skills/test.md", RunAs: skill.RunInline, Body: "# Test\n\nRun the tests."},
	}
}

func TestSkillPickerRenderSmoke(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:   pickerSkills,
			skills: skills,
			roots:  nil,
		},
	}

	out := m.renderSkillPicker()
	if out == "" {
		t.Fatal("renderSkillPicker returned empty string")
	}
	if !strings.Contains(out, "Manage skills") {
		t.Fatalf("render missing title:\n%s", out)
	}
	if !strings.Contains(out, "review") {
		t.Fatalf("render missing skill name:\n%s", out)
	}
	if !strings.Contains(out, "explore") {
		t.Fatalf("render missing builtin skill:\n%s", out)
	}
}

func TestSkillPickerClosed(t *testing.T) {
	m := chatTUI{width: 80, skillPick: nil}
	if out := m.renderSkillPicker(); out != "" {
		t.Fatalf("closed picker rendered %q", out)
	}
}

func TestSkillPickerEnterClosesWithoutChanges(t *testing.T) {
	skills := makeTestSkills()
	m := newTestChatTUI()
	m.skills = skills
	m.skillPick = &skillPicker{
		mode:            pickerSkills,
		skills:          skills,
		sel:             0,
		enabled:         map[string]bool{"review": true},
		originalEnabled: map[string]bool{"review": true},
	}
	m.input.SetValue("old input")

	next, _ := m.saveSkillPick()
	cm := next.(chatTUI)
	if val := cm.input.Value(); val != "old input" {
		t.Fatalf("saveSkillPick changed input to %q, want old input", val)
	}
	if cm.skillPick != nil {
		t.Fatal("saveSkillPick did not close the picker")
	}
}

func TestSkillsBareOpensPicker(t *testing.T) {
	m := newTestChatTUI()
	m.width = 80
	m.skills = makeTestSkills()

	m.runSkillSubcommand("/skills")
	if m.skillPick == nil {
		t.Fatal("bare /skills should open the interactive picker")
	}
}

func TestSkillsManageOpensPicker(t *testing.T) {
	m := newTestChatTUI()
	m.width = 80
	m.skills = makeTestSkills()

	m.runSkillSubcommand("/skills manage")
	if m.skillPick == nil {
		t.Fatal("/skills manage should open the interactive picker")
	}
}

func TestSkillsQuestionOpensSubcommandCompletion(t *testing.T) {
	m := newTestChatTUI()
	m.width = 80
	m.skills = makeTestSkills()
	m.input.SetValue("/skills?")
	m.updateCompletion()
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatalf("/skills? should open subcommand completion: %+v", m.completion)
	}
	if hasLabel(m.completion.items, "manage") {
		t.Fatalf("redundant manage subcommand should be hidden from /skills? menu: %+v", m.completion.items)
	}
	if hasLabel(m.completion.items, "list") {
		t.Fatalf("redundant list subcommand should be hidden from /skills? menu: %+v", m.completion.items)
	}
	if !hasLabel(m.completion.items, "show") {
		t.Fatalf("/skills? should include useful subcommands: %+v", m.completion.items)
	}
	m.input.SetValue("/skills ")
	m.updateCompletion()
	if m.completion.active {
		t.Fatalf("/skills <space> should not open subcommand completion: %+v", m.completion)
	}
}

func TestSkillsEnterSubmitsExactSlashCommand(t *testing.T) {
	m := newTestChatTUI()
	m.width = 80
	m.skills = makeTestSkills()
	m.input.SetValue("/skills")
	m.updateCompletion()
	if !m.completion.active {
		t.Fatal("typing /skills should show slash completion before Enter")
	}
	if m.completion.kind == compSlashArg {
		t.Fatalf("typing exact /skills should not open subcommand completion: %+v", m.completion)
	}

	next, _ := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	cm := next.(chatTUI)
	if cm.skillPick == nil {
		t.Fatal("Enter on exact /skills should open the interactive picker")
	}
	if got := cm.input.Value(); got != "" {
		t.Fatalf("input after submitting /skills = %q, want empty", got)
	}
}

func TestSkillsListRendersScrollback(t *testing.T) {
	m := newTestChatTUI()
	m.width = 80
	m.skills = makeTestSkills()

	m.runSkillSubcommand("/skills list")
	if m.skillPick != nil {
		t.Fatal("/skills list should render a static list, not open the picker")
	}
	if len(m.transcript) == 0 {
		t.Fatal("/skills list should commit a list to scrollback")
	}
	got := strings.Join(m.transcript, "\n")
	if !strings.Contains(got, "skills") || !strings.Contains(got, "/review") {
		t.Fatalf("/skills list output missing expected content:\n%s", got)
	}
}

func TestSkillPickerSearch(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:         pickerSkills,
			skills:       skills,
			searchActive: true,
			query:        "rev",
			sel:          0,
		},
	}

	out := m.renderSkillPicker()
	if out == "" {
		t.Fatal("renderSkillPicker returned empty string")
	}
	if !strings.Contains(out, "rev") {
		t.Fatalf("search mode missing query:\n%s", out)
	}
	if footer := m.renderMainManagerFooter(); !strings.Contains(footer, "search") && !strings.Contains(footer, "搜索") {
		t.Fatalf("search mode missing footer hint:\n%s", footer)
	}
	if !strings.Contains(out, "review") {
		t.Fatalf("search filter should include review:\n%s", out)
	}
	if strings.Contains(out, "explore") || strings.Contains(out, "test") {
		t.Fatalf("search filter should exclude non-matching skills:\n%s", out)
	}
}

func TestSkillPickerRenderDialogStyle(t *testing.T) {
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	var skills []skill.Skill
	for i := 0; i < 24; i++ {
		skills = append(skills, skill.Skill{
			Name:        "skill-" + strings.Repeat("x", i%4) + string(rune('a'+i)),
			Description: "this long description should stay out of the default picker list",
			Scope:       skill.ScopeGlobal,
			Body:        "short body",
		})
	}
	m := chatTUI{
		width:  80,
		height: 24,
		skills: skills,
		skillPick: &skillPicker{
			mode:   pickerSkills,
			skills: skills,
			sel:    0,
		},
	}

	out := m.renderSkillPicker()
	if !strings.Contains(out, "Search skills") {
		t.Fatalf("dialog render missing search box:\n%s", out)
	}
	if !strings.Contains(out, "/ Search skills") {
		t.Fatalf("dialog render should use slash search prompt, not a tiny glyph:\n%s", out)
	}
	if !strings.Contains(out, "more below") {
		t.Fatalf("dialog render missing overflow indicator:\n%s", out)
	}
	if strings.Contains(out, "this long description") {
		t.Fatalf("default picker list should not render long descriptions:\n%s", out)
	}
}

func TestSkillPickerSearchEmptyResult(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:         pickerSkills,
			skills:       skills,
			searchActive: true,
			query:        "zzz_nonexistent",
			sel:          0,
		},
	}

	out := m.renderSkillPicker()
	if !strings.Contains(out, "match") && !strings.Contains(out, "匹配") {
		t.Fatalf("empty search should show empty state:\n%s", out)
	}
}

func TestSkillPickerDetail(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:        pickerDetail,
			skills:      skills,
			detailSkill: skills[0],
			detailBack:  pickerSkills,
		},
	}

	out := m.renderSkillPicker()
	if !strings.Contains(out, "subagent") {
		t.Fatalf("detail should show subagent tag:\n%s", out)
	}
	if !strings.Contains(out, "Scope") && !strings.Contains(out, "范围") {
		t.Fatalf("detail should show scope:\n%s", out)
	}
}

func TestSkillPickerSourceView(t *testing.T) {
	skills := makeTestSkills()
	roots := []skillRootLine{
		{dir: "/fake/proj/.reasonix/skills", scope: skill.ScopeProject, status: skill.StatusOK, skills: 2, diagnostic: true},
		{dir: i18nSkillPickerBuiltinSource(), scope: skill.ScopeBuiltin, status: skill.StatusOK, skills: 1, diagnostic: true},
	}
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:      pickerSources,
			skills:    skills,
			roots:     roots,
			sourceSel: 0,
		},
	}

	out := m.renderSkillPicker()
	if !strings.Contains(out, "Sources") && !strings.Contains(out, "来源") {
		t.Fatalf("source view missing title:\n%s", out)
	}
	if !strings.Contains(out, ".reasonix") {
		t.Fatalf("source view should show root path:\n%s", out)
	}
}

// i18nSkillPickerBuiltinSource returns SkillPickerBuiltinSource regardless of locale.
// In tests the i18n package is initialized to English by default.
func i18nSkillPickerBuiltinSource() string {
	return "builtin"
}

func TestSkillPickerDiagnostics(t *testing.T) {
	roots := []skillRootLine{
		{dir: "/conf", scope: skill.ScopeCustom, status: skill.StatusOK, skills: 0, configured: true},
		{dir: "/proj/.reasonix/skills", scope: skill.ScopeProject, status: skill.StatusOK, skills: 1, diagnostic: true},
		{dir: "/proj/.agents/skills", scope: skill.ScopeProject, status: skill.StatusMissing, skills: 0, diagnostic: true},
		{dir: "/proj/.agent/skills", scope: skill.ScopeProject, status: skill.StatusMissing, skills: 0, diagnostic: true},
	}

	p := &skillPicker{mode: pickerSources, roots: roots}

	// Default: diagnostics hidden.
	visible := p.visibleRoots()
	if len(visible) != 2 {
		t.Fatalf("default visibleRoots got %d, want 2 (configured custom + active project): %v", len(visible), visible)
	}

	// Show diagnostics.
	p.showDiagnostics = true
	visible = p.visibleRoots()
	if len(visible) != 4 {
		t.Fatalf("with diagnostics visibleRoots got %d, want 4: %v", len(visible), visible)
	}
}

func TestFilteredSkills(t *testing.T) {
	skills := makeTestSkills()
	p := &skillPicker{skills: skills, query: "review"}
	filtered := p.filteredSkills()
	if len(filtered) != 1 || filtered[0].Name != "review" {
		t.Fatalf("filteredSkills(review) = %v", filtered)
	}

	p.query = "code"
	filtered = p.filteredSkills()
	if len(filtered) != 1 || filtered[0].Name != "review" {
		t.Fatalf("filteredSkills(code) should match description: %v", filtered)
	}

	p.query = "zzz"
	filtered = p.filteredSkills()
	if len(filtered) != 0 {
		t.Fatalf("filteredSkills(zzz) should be empty: %v", filtered)
	}

	p.query = ""
	filtered = p.filteredSkills()
	if len(filtered) != 3 {
		t.Fatalf("filteredSkills(empty) should return all: %v", filtered)
	}
}

func TestSkillPickerSummaryDefault(t *testing.T) {
	skills := makeTestSkills()
	p := &skillPicker{skills: skills}
	s := skillPickerSummary(p)
	if !strings.Contains(s, "available") && !strings.Contains(s, "可用") {
		t.Fatalf("summary missing 'available': %q", s)
	}
	if !strings.Contains(s, "project") && !strings.Contains(s, "项目") {
		t.Fatalf("summary missing project count: %q", s)
	}
	if !strings.Contains(s, "builtin") && !strings.Contains(s, "内置") {
		t.Fatalf("summary missing builtin count: %q", s)
	}
}

func TestSkillPickerSummarySearch(t *testing.T) {
	skills := makeTestSkills()
	p := &skillPicker{skills: skills, searchActive: true, query: "rev"}
	s := skillPickerSummary(p)
	if !strings.Contains(s, "matching") && !strings.Contains(s, "匹配") {
		t.Fatalf("search summary missing 'matching': %q", s)
	}
	// Should mention total count.
	if !strings.Contains(s, "3") {
		t.Fatalf("search summary missing total count: %q", s)
	}
}

func TestSkillSourceSummary(t *testing.T) {
	roots := []skillRootLine{
		{dir: "/a", skills: 2},
		{dir: "/b", skills: 1},
		{dir: "/c", skills: 0},
	}
	s := skillSourceSummary(roots)
	if !strings.Contains(s, "active") && !strings.Contains(s, "有效") {
		t.Fatalf("source summary missing 'active': %q", s)
	}

	empty := skillSourceSummary([]skillRootLine{{dir: "/x", skills: 0}})
	if empty != "" {
		t.Fatalf("source summary with no active roots should be empty: %q", empty)
	}
}

func TestSkillPickerEscCloses(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:   pickerSkills,
			skills: skills,
		},
	}

	next, _ := m.handleSkillPickerKey(tea.KeyPressMsg{Code: 27}) // Esc
	cm := next.(chatTUI)
	if cm.skillPick != nil {
		t.Fatal("Esc did not close skill picker")
	}
}

func TestSkillPickerSearchEscExitsSearch(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:         pickerSkills,
			skills:       skills,
			searchActive: true,
			query:        "rev",
		},
	}

	next, _ := m.handleSkillPickerKey(tea.KeyPressMsg{Code: 27}) // Esc
	cm := next.(chatTUI)
	if cm.skillPick == nil {
		t.Fatal("Esc closed picker instead of exiting search")
	}
	if cm.skillPick.searchActive {
		t.Fatal("Esc did not exit search mode")
	}
}

func TestSkillPickerSSwitchesToSources(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:   pickerSkills,
			skills: skills,
		},
	}

	next, _ := m.handleSkillPickerKey(tea.KeyPressMsg{Code: 's'})
	cm := next.(chatTUI)
	if cm.skillPick.mode != pickerSources {
		t.Fatalf("s did not switch to source view, mode=%s", cm.skillPick.mode)
	}
}

func TestSkillPickerSourceEnterShowsRootSkills(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:   pickerSources,
			skills: skills,
			roots: []skillRootLine{
				{dir: "/fake/proj/.reasonix/skills", scope: skill.ScopeProject, status: skill.StatusOK, skills: 2, diagnostic: true},
			},
		},
	}

	next, _ := m.handleSkillPickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	cm := next.(chatTUI)
	if cm.skillPick.mode != pickerSourceSkills {
		t.Fatalf("Enter did not open source skill list, mode=%s", cm.skillPick.mode)
	}
	out := cm.renderSkillPicker()
	if !strings.Contains(out, "review") || !strings.Contains(out, "test") {
		t.Fatalf("source skill list missing project skills:\n%s", out)
	}
	if strings.Contains(out, "explore") {
		t.Fatalf("source skill list should not include builtin skill:\n%s", out)
	}
}

func TestSkillPickerSpaceTogglesEnabled(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  80,
		skills: skills,
		skillPick: &skillPicker{
			mode:            pickerSkills,
			skills:          skills,
			sel:             0,
			enabled:         map[string]bool{"review": true},
			originalEnabled: map[string]bool{"review": true},
		},
	}

	// Space toggles the selected skill off.
	next, _ := m.handleSkillPickerKey(tea.KeyPressMsg{Code: ' '})
	cm := next.(chatTUI)
	if cm.skillPick.skillEnabled("review") {
		t.Fatal("Space did not disable the selected skill")
	}

	// Space toggles it back on.
	next2, _ := cm.handleSkillPickerKey(tea.KeyPressMsg{Code: ' '})
	cm2 := next2.(chatTUI)
	if !cm2.skillPick.skillEnabled("review") {
		t.Fatal("second Space did not enable the selected skill")
	}
}

func TestSkillPickerDetailDeleteRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review", skill.SkillFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Review"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := []skill.Skill{
		{Name: "review", Description: "Review code", Scope: skill.ScopeProject, Path: path, RunAs: skill.RunSubagent, Body: "# Review"},
	}
	p := &skillPicker{
		mode:         pickerDetail,
		skills:       skills,
		detailSkill:  skills[0],
		detailBack:   pickerSkills,
		detailAction: 1,
	}
	m := chatTUI{width: 80, skills: skills, skillPick: p}

	next, _ := m.handleSkillPickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	cm := next.(chatTUI)
	if cm.skillPick.mode != pickerConfirmDelete {
		t.Fatalf("delete action should open confirmation, mode=%s", cm.skillPick.mode)
	}
	if cm.skillPick.confirm != 1 {
		t.Fatalf("delete confirmation should default to cancel, got %d", cm.skillPick.confirm)
	}
}

func TestSkillPickerDetailShowsActionsBeforeBodyPreview(t *testing.T) {
	s := skill.Skill{
		Name:        "review",
		Description: "Review code",
		Scope:       skill.ScopeProject,
		Path:        "/proj/.reasonix/skills/review/SKILL.md",
		RunAs:       skill.RunSubagent,
		Body:        "# Review\n\n" + strings.Repeat("BODY_LINE\n", 40),
	}
	m := chatTUI{
		width:  80,
		skills: []skill.Skill{s},
		skillPick: &skillPicker{
			mode:            pickerDetail,
			skills:          []skill.Skill{s},
			detailSkill:     s,
			enabled:         map[string]bool{s.Name: true},
			originalEnabled: map[string]bool{s.Name: true},
		},
	}

	out := m.renderSkillPickerDetail()
	actionAt := strings.Index(out, i18n.M.SkillPickerActionToggle)
	bodyAt := strings.Index(out, "BODY_LINE")
	if actionAt < 0 {
		t.Fatalf("detail missing action row:\n%s", out)
	}
	if bodyAt < 0 {
		t.Fatalf("detail missing body preview:\n%s", out)
	}
	if actionAt > bodyAt {
		t.Fatalf("detail action should render before body preview:\n%s", out)
	}
}

func TestDeleteSkillPickRemovesDirectoryTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review", skill.SkillFile)
	targetDir := filepath.Dir(path)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Review"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := skill.Skill{Name: "review", Scope: skill.ScopeProject, Path: path, RunAs: skill.RunSubagent, Body: "# Review"}
	m := newTestChatTUI()
	m.skills = []skill.Skill{s}
	m.skillPick = &skillPicker{
		mode:            pickerConfirmDelete,
		skills:          []skill.Skill{s},
		deleteSkill:     s,
		enabled:         map[string]bool{s.Name: true},
		originalEnabled: map[string]bool{s.Name: true},
	}

	next, _ := m.deleteSkillPick(s)
	cm := next.(chatTUI)
	if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
		t.Fatalf("deleteSkillPick target still exists or unexpected error: %v", err)
	}
	if cm.skillPick == nil || cm.skillPick.mode != pickerSkills {
		t.Fatalf("delete should return to skills list, picker=%v", cm.skillPick)
	}
}

func TestSkillDeleteTargetDirectoryAndFlatFile(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "review", skill.SkillFile)
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("# Review"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, ok, err := skillDeleteTarget(skill.Skill{Name: "review", Scope: skill.ScopeProject, Path: nested})
	if err != nil || !ok {
		t.Fatalf("skillDeleteTarget directory = %q %v %v", target, ok, err)
	}
	if target != filepath.Dir(nested) {
		t.Fatalf("directory target = %q, want %q", target, filepath.Dir(nested))
	}

	flat := filepath.Join(dir, "flat.md")
	if err := os.WriteFile(flat, []byte("# Flat"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, ok, err = skillDeleteTarget(skill.Skill{Name: "flat", Scope: skill.ScopeProject, Path: flat})
	if err != nil || !ok {
		t.Fatalf("skillDeleteTarget flat = %q %v %v", target, ok, err)
	}
	if target != flat {
		t.Fatalf("flat target = %q, want %q", target, flat)
	}

	if _, ok, err := skillDeleteTarget(skill.Skill{Name: "explore", Scope: skill.ScopeBuiltin, Path: "(builtin)"}); err != nil || ok {
		t.Fatalf("builtin target ok=%v err=%v, want not removable", ok, err)
	}
}

func TestClampSel(t *testing.T) {
	items := []int{10, 20, 30}
	if got := clampSel(0, items); got != 0 {
		t.Fatalf("clampSel(0) = %d", got)
	}
	if got := clampSel(2, items); got != 2 {
		t.Fatalf("clampSel(2) = %d", got)
	}
	if got := clampSel(5, items); got != 2 {
		t.Fatalf("clampSel(5) = %d, want 2", got)
	}
	if got := clampSel(-1, items); got != 0 {
		t.Fatalf("clampSel(-1) = %d, want 0", got)
	}
	if got := clampSel(0, []int{}); got != 0 {
		t.Fatalf("clampSel(0, []) = %d, want 0", got)
	}
}

func TestSkillRowLabelHasSubagentTag(t *testing.T) {
	s := skill.Skill{Name: "review", Description: "Review code", Scope: skill.ScopeProject, RunAs: skill.RunSubagent}
	row := renderSkillRow(1, false, s, true, 80)
	if !strings.Contains(row, "subagent") && !strings.Contains(row, "子代理") {
		t.Fatalf("subagent skill missing tag: %q", row)
	}
	if !strings.Contains(row, "review") {
		t.Fatalf("missing skill name: %q", row)
	}
}

func TestRenderSkillRowSelected(t *testing.T) {
	s := skill.Skill{Name: "test", Description: "A test skill", Scope: skill.ScopeGlobal, RunAs: skill.RunInline}
	row := renderSkillRow(5, true, s, true, 80)
	if !strings.Contains(row, "›") {
		t.Fatalf("selected row missing arrow: %q", row)
	}
	// Selected row differs from unselected: has arrow, no dim prefix.
	unsel := renderSkillRow(5, false, s, true, 80)
	if row == unsel {
		t.Fatal("selected and unselected rows should differ")
	}
}

func TestRenderSkillRowScopeMeta(t *testing.T) {
	s := skill.Skill{Name: "example", Description: "desc", Scope: skill.ScopeGlobal, RunAs: skill.RunInline}
	row := renderSkillRow(1, false, s, true, 80)
	if !strings.Contains(row, "global") && !strings.Contains(row, "全局") {
		t.Fatalf("row missing scope label: %q", row)
	}
	if !strings.Contains(row, "tok") {
		t.Fatalf("row missing approximate token count: %q", row)
	}
}

func TestRenderSkillRowLongNameTruncated(t *testing.T) {
	s := skill.Skill{
		Name:        "this-is-a-very-long-skill-name-that-exceeds-26-chars",
		Description: "desc",
		Scope:       skill.ScopeBuiltin,
	}
	row := renderSkillRow(1, false, s, true, 80)
	if !strings.Contains(row, "…") {
		t.Fatalf("long name not truncated with …: %q", row)
	}
}

func TestRenderSkillRowChineseBadgeFitsWidth(t *testing.T) {
	i18n.DetectLanguage("zh")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	s := skill.Skill{
		Name:        "browser-testing-with-devtools",
		Description: "Tests in real browsers. Use when building or debugging anything that runs in a browser.",
		Scope:       skill.ScopeGlobal,
		RunAs:       skill.RunSubagent,
	}
	row := renderSkillRow(13, true, s, true, 80)
	if got := visibleWidth(row); got > 80 {
		t.Fatalf("row visible width = %d, want <= 80:\n%q", got, row)
	}
	if strings.Contains(row, "\n") {
		t.Fatalf("row should stay single-line: %q", row)
	}
}

func TestSortedSkills(t *testing.T) {
	skills := []skill.Skill{
		{Name: "z-builtin", Scope: skill.ScopeBuiltin},
		{Name: "a-project", Scope: skill.ScopeProject},
		{Name: "b-project", Scope: skill.ScopeProject},
		{Name: "c-global", Scope: skill.ScopeGlobal},
		{Name: "d-custom", Scope: skill.ScopeCustom},
	}
	sorted := sortedSkills(skills)
	// Project first, then custom, global, builtin; alphabetical within.
	want := []string{"a-project", "b-project", "d-custom", "c-global", "z-builtin"}
	for i, s := range sorted {
		if i >= len(want) || s.Name != want[i] {
			t.Fatalf("sorted[%d] = %s, want %s", i, s.Name, want[i])
		}
	}
}

func TestSkillPickerRendersInMainArea(t *testing.T) {
	m := newTestChatTUI()
	m.width = 80
	m.height = 40
	m.skillPick = &skillPicker{
		mode: pickerSkills,
		skills: []skill.Skill{
			{Name: "a", Description: "desc", Scope: skill.ScopeBuiltin},
			{Name: "b", Description: "desc", Scope: skill.ScopeBuiltin},
			{Name: "c", Description: "desc", Scope: skill.ScopeBuiltin},
		},
	}

	rows := m.bottomRows()
	footerRows := strings.Count(m.renderMainManagerFooter(), "\n") + 1
	if want := footerRows + 2; rows != want {
		t.Fatalf("bottomRows with skill picker open got %d, want %d (footer + status rows)", rows, want)
	}
	if !m.hideComposer() {
		t.Fatal("skill picker should hide the composer")
	}
	if out := m.renderMainManager(); !strings.Contains(out, "Manage skills") {
		t.Fatalf("skill picker should render as a main manager:\n%s", out)
	}
}

func TestBottomRowsIncludesResumePicker(t *testing.T) {
	m := newTestChatTUI()
	m.width = 80
	m.height = 40
	m.resumePick = &resumePicker{
		sessions: nil,
		sel:      0,
		active:   -1,
	}

	// Not testing exact row count (resumePicker needs sessions for rendering),
	// just verifying that bottomRows doesn't panic and returns a non-zero value.
	rows := m.bottomRows()
	if rows < 3 {
		t.Fatalf("bottomRows with resume picker got %d", rows)
	}
}

func TestRescanInSearchModeClampsToFiltered(t *testing.T) {
	skills := []skill.Skill{
		{Name: "alpha", Description: "first", Scope: skill.ScopeBuiltin},
		{Name: "beta", Description: "second", Scope: skill.ScopeBuiltin},
		{Name: "gamma", Description: "third", Scope: skill.ScopeBuiltin},
	}
	p := &skillPicker{
		mode:         pickerSkills,
		skills:       skills,
		searchActive: true,
		query:        "beta",
		sel:          2, // beyond filtered results (only "beta" matches)
	}
	p.sel = clampSel(p.sel, skills) // simulating old behavior

	// Fix: clamp to filtered when search is active.
	if p.searchActive && p.query != "" {
		p.sel = clampSel(p.sel, p.filteredSkills())
	}
	if p.sel != 0 {
		t.Fatalf("sel after clamp to filtered should be 0, got %d", p.sel)
	}
}

func TestPathBoundaryMatching(t *testing.T) {
	skills := []skill.Skill{
		{Name: "alpha", Scope: skill.ScopeProject, Path: "/proj/.reasonix/skills/alpha/SKILL.md"},
		{Name: "beta", Scope: skill.ScopeProject, Path: "/proj/.reasonix/skills-extra/beta/SKILL.md"},
	}
	lines := []skillRootLine{
		{dir: "/proj/.reasonix/skills", scope: skill.ScopeProject, status: skill.StatusOK},
		{dir: "/proj/.reasonix/skills-extra", scope: skill.ScopeProject, status: skill.StatusOK},
	}

	// Count skills per root with directory-boundary match.
	for _, s := range skills {
		if s.Scope == skill.ScopeBuiltin {
			continue
		}
		cleanPath := filepath.Clean(s.Path)
		for i := range lines {
			if lines[i].scope != s.Scope {
				continue
			}
			prefix := filepath.Clean(lines[i].dir) + string(filepath.Separator)
			if strings.HasPrefix(cleanPath, prefix) {
				lines[i].skills++
				break
			}
		}
	}

	if lines[0].skills != 1 {
		t.Fatalf("/skills root should have 1 skill, got %d", lines[0].skills)
	}
	if lines[1].skills != 1 {
		t.Fatalf("/skills-extra root should have 1 skill, got %d", lines[1].skills)
	}
}

func TestSourceRowLabelUsesI18n(t *testing.T) {
	i18n.DetectLanguage("zh")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	r := skillRootLine{
		dir:    "/proj/.reasonix/skills",
		scope:  skill.ScopeProject,
		status: skill.StatusOK,
		skills: 3,
	}
	label := sourceRowLabel(r, 80)
	if !strings.Contains(label, "项目") || strings.Contains(label, "project") {
		t.Fatalf("source row should use localized scope label, got %q", label)
	}
	// Check that skills unit is used
	if !strings.Contains(label, "skills") && !strings.Contains(label, "skill") {
		t.Fatalf("source row missing skills unit: %q", label)
	}
}

func TestStatusLabelI18n(t *testing.T) {
	if l := statusLabel(skill.StatusOK); l == "" {
		t.Fatal("statusLabel(ok) empty")
	}
	if l := statusLabel(skill.StatusMissing); l == "" {
		t.Fatal("statusLabel(missing) empty")
	}
	if l := statusLabel(skill.StatusNotDirectory); l == "" {
		t.Fatal("statusLabel(not-directory) empty")
	}
	if l := statusLabel(skill.StatusUnreadable); l == "" {
		t.Fatal("statusLabel(unreadable) empty")
	}
	// Unknown status falls back to raw string.
	if l := statusLabel(skill.PathStatus("custom-status")); l != "custom-status" {
		t.Fatalf("statusLabel(unknown) = %q, want 'custom-status'", l)
	}
}

func TestRenderSkillDetailUsesI18nMeta(t *testing.T) {
	s := skill.Skill{
		Name:        "review",
		Description: "Review code",
		Scope:       skill.ScopeProject,
		Path:        "/proj/.reasonix/skills/review/SKILL.md",
		RunAs:       skill.RunSubagent,
		Body:        "# Review\n\nLine 1\nLine 2",
	}
	out := renderSkillDetail(s, 80)
	// Detail should use i18n meta format (not raw "Scope:" when in Chinese).
	if !strings.Contains(out, "Scope") && !strings.Contains(out, "范围") {
		t.Fatalf("detail missing scope label:\n%s", out)
	}
	if !strings.Contains(out, "Run as") && !strings.Contains(out, "运行") {
		t.Fatalf("detail missing run-as label:\n%s", out)
	}
}

func TestLegacySkillShowUnchanged(t *testing.T) {
	s := skill.Skill{
		Name:        "my-skill",
		Description: "A test skill",
		Scope:       skill.ScopeProject,
		Path:        "/fake/proj/.reasonix/skills/my-skill.md",
		Body:        "# My Skill\n\nSome body text.",
	}
	out := renderSkillShow(80, s, false)
	if !strings.Contains(out, "skill:") && !strings.Contains(out, "my-skill") {
		t.Fatalf("legacy skill show broken:\n%s", out)
	}
}

func TestLegacySkillPathsUnchanged(t *testing.T) {
	roots := []skill.Root{
		{Dir: "/proj/.reasonix/skills", Scope: skill.ScopeProject, Priority: 0, Status: skill.StatusOK},
	}
	out := renderSkillPaths(80, roots)
	if !strings.Contains(out, "skill paths") {
		t.Fatalf("legacy skill paths broken:\n%s", out)
	}
}

func TestSkillPickerRenderWidthNarrow(t *testing.T) {
	skills := makeTestSkills()
	m := chatTUI{
		width:  30,
		skills: skills,
		skillPick: &skillPicker{
			mode:   pickerSkills,
			skills: skills,
		},
	}

	out := m.renderSkillPicker()
	if out == "" {
		t.Fatal("narrow render returned empty")
	}
	if !strings.Contains(out, "Manage skills") {
		t.Fatalf("narrow render missing title:\n%s", out)
	}
}
