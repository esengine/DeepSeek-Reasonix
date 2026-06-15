package cli

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

type cliColor struct {
	hex   string
	xterm int
}

type cliPalette struct {
	name         string
	style        string
	accent       cliColor
	muted        cliColor
	faint        cliColor
	success      cliColor
	warn         cliColor
	err          cliColor
	danger       cliColor
	border       cliColor
	selection    cliColor
	userBubbleBG cliColor
	diffAddBG    cliColor
	diffDelBG    cliColor
	toolRead     cliColor
	toolProc     cliColor
}

type cliThemeStyle struct {
	name        string
	mode        string
	accent      cliColor
	description string
}

var (
	cliDarkTheme = cliPalette{
		name:         "dark",
		style:        "graphite",
		accent:       cliColor{"#d97757", 173},
		muted:        cliColor{"#c0c4cc", 251},
		faint:        cliColor{"#858b96", 245},
		success:      cliColor{"#74b87a", 108},
		warn:         cliColor{"#d9a441", 179},
		err:          cliColor{"#e0696a", 167},
		danger:       cliColor{"#e5484d", 167},
		border:       cliColor{"#343945", 237},
		selection:    cliColor{"#d97757", 173},
		userBubbleBG: cliColor{"#222631", 235},
		diffAddBG:    cliColor{"#14351d", 22},
		diffDelBG:    cliColor{"#3a1619", 52},
		toolRead:     cliColor{"#56b6c2", 80},
		toolProc:     cliColor{"#c678dd", 176},
	}
	cliLightTheme = cliPalette{
		name:         "light",
		style:        "sandstone",
		accent:       cliColor{"#2f5fa8", 25},
		muted:        cliColor{"#555049", 239},
		faint:        cliColor{"#82796f", 243},
		success:      cliColor{"#5d9b66", 65},
		warn:         cliColor{"#b68120", 136},
		err:          cliColor{"#b94b4d", 131},
		danger:       cliColor{"#e5484d", 167},
		border:       cliColor{"#ded4c6", 252},
		selection:    cliColor{"#6f91d9", 68},
		userBubbleBG: cliColor{"#f5f0e8", 255},
		diffAddBG:    cliColor{"#e5f3e7", 254},
		diffDelBG:    cliColor{"#fae8e8", 255},
		toolRead:     cliColor{"#6f91d9", 68},
		toolProc:     cliColor{"#8a6bb8", 97},
	}
	cliThemeStyles = []cliThemeStyle{
		{name: "graphite", mode: "dark", accent: cliColor{"#d97757", 173}, description: "warm clay accent"},
		{name: "ember", mode: "dark", accent: cliColor{"#f06d38", 209}, description: "hot orange accent"},
		{name: "aurora", mode: "dark", accent: cliColor{"#34c3a6", 79}, description: "cool teal accent"},
		{name: "midnight", mode: "dark", accent: cliColor{"#b18cff", 141}, description: "quiet violet accent"},
		{name: "sandstone", mode: "light", accent: cliColor{"#c2613f", 173}, description: "default warm light accent"},
		{name: "porcelain", mode: "light", accent: cliColor{"#7d63c8", 104}, description: "soft violet light accent"},
		{name: "linen", mode: "light", accent: cliColor{"#bd5d4d", 167}, description: "muted coral light accent"},
		{name: "glacier", mode: "light", accent: cliColor{"#357fa8", 74}, description: "cool blue light accent"},
	}
	activeCLITheme                  = applyCLIThemeStyle(cliDarkTheme, cliThemeStyles[0])
	queryTerminalBackgroundForTheme = queryTerminalBackground
)

func configureCLITheme(mode string) {
	configureCLIThemeWithStyle(mode, "")
}

func configureCLIThemeWithStyle(mode, style string) {
	if env := strings.TrimSpace(os.Getenv("REASONIX_THEME")); env != "" {
		if st, ok := cliThemeStyleByName(env); ok {
			mode = st.mode
			style = st.name
		} else {
			mode = env
		}
	}
	if env := strings.TrimSpace(os.Getenv("REASONIX_THEME_STYLE")); env != "" {
		style = env
	}
	activeCLITheme = resolveCLIThemeWithStyle(mode, style)
	refreshCLIStyles()
}

func resolveCLITheme(mode string) cliPalette {
	return resolveCLIThemeWithStyle(mode, "")
}

func resolveCLIThemeWithStyle(mode, style string) cliPalette {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if st, ok := cliThemeStyleByName(mode); ok {
		return buildCLITheme(st.mode, st.name)
	}
	resolvedMode := resolveCLIThemeMode(mode)
	st, ok := cliThemeStyleByName(style)
	if !ok || st.mode != resolvedMode {
		st = defaultCLIThemeStyle(resolvedMode)
	}
	return buildCLITheme(resolvedMode, st.name)
}

func resolveCLIThemeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "light":
		return "light"
	case "dark":
		return "dark"
	case "auto", "":
		if rgb, ok := queryTerminalBackgroundForTheme(); ok {
			if rgb.looksLight() {
				return "light"
			}
			return "dark"
		}
		if colorFGBGLooksLight() {
			return "light"
		}
		return "dark"
	default:
		return "dark"
	}
}

func buildCLITheme(mode, style string) cliPalette {
	base := cliDarkTheme
	if mode == "light" {
		base = cliLightTheme
	}
	st, ok := cliThemeStyleByName(style)
	if !ok || st.mode != base.name {
		st = defaultCLIThemeStyle(base.name)
	}
	return applyCLIThemeStyle(base, st)
}

func applyCLIThemeStyle(base cliPalette, style cliThemeStyle) cliPalette {
	base.style = style.name
	base.accent = style.accent
	base.selection = style.accent
	return base
}

func cliThemeStyleByName(name string) (cliThemeStyle, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, st := range cliThemeStyles {
		if st.name == name {
			return st, true
		}
	}
	return cliThemeStyle{}, false
}

func defaultCLIThemeStyle(mode string) cliThemeStyle {
	if mode == "light" {
		for _, st := range cliThemeStyles {
			if st.name == "sandstone" {
				return st
			}
		}
	}
	return cliThemeStyles[0]
}

// withoutTerminalProbe resolves a theme with the OSC background probe disabled —
// for callers running while something else (the live TUI) owns stdin, where a
// raw-mode read would fight the TUI's input reader. "auto" then falls back to the
// COLORFGBG heuristic.
func withoutTerminalProbe(fn func()) {
	prev := queryTerminalBackgroundForTheme
	queryTerminalBackgroundForTheme = func() (terminalRGB, bool) { return terminalRGB{}, false }
	defer func() { queryTerminalBackgroundForTheme = prev }()
	fn()
}

func setCLIThemeMode(mode string) cliPalette {
	// A runtime /theme switch runs inside the TUI, which owns stdin, so resolving
	// "auto" must not live-probe the terminal here.
	withoutTerminalProbe(func() {
		activeCLITheme = resolveCLIThemeWithStyle(mode, activeCLITheme.style)
	})
	refreshCLIStyles()
	return activeCLITheme
}

func setCLIThemeStyle(name string) (cliPalette, bool) {
	st, ok := cliThemeStyleByName(name)
	if !ok {
		return cliPalette{}, false
	}
	activeCLITheme = resolveCLIThemeWithStyle(st.mode, st.name)
	refreshCLIStyles()
	return activeCLITheme, true
}

type terminalRGB struct {
	r int
	g int
	b int
}

func (c terminalRGB) looksLight() bool {
	luma := 0.2126*float64(c.r) + 0.7152*float64(c.g) + 0.0722*float64(c.b)
	return luma >= 150
}

func parseOSC11Response(s string) (terminalRGB, bool) {
	idx := strings.Index(s, "]11;")
	if idx < 0 {
		return terminalRGB{}, false
	}
	payload := s[idx+len("]11;"):]
	if end := strings.IndexByte(payload, '\a'); end >= 0 {
		payload = payload[:end]
	} else if end := strings.Index(payload, "\x1b\\"); end >= 0 {
		payload = payload[:end]
	}
	payload = strings.TrimSpace(payload)
	if strings.HasPrefix(payload, "#") {
		r, g, b, ok := parseHexColor(payload)
		return terminalRGB{r, g, b}, ok
	}
	for _, prefix := range []string{"rgb:", "rgba:"} {
		if strings.HasPrefix(payload, prefix) {
			return parseOSCColorTriplet(strings.TrimPrefix(payload, prefix))
		}
	}
	return terminalRGB{}, false
}

func parseOSCColorTriplet(s string) (terminalRGB, bool) {
	parts := strings.Split(s, "/")
	if len(parts) < 3 {
		return terminalRGB{}, false
	}
	r, okR := parseOSCColorComponent(parts[0])
	g, okG := parseOSCColorComponent(parts[1])
	b, okB := parseOSCColorComponent(parts[2])
	return terminalRGB{r, g, b}, okR && okG && okB
}

func parseOSCColorComponent(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 4 {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return 0, false
	}
	max := int64(1)<<(4*len(s)) - 1
	if max <= 0 {
		return 0, false
	}
	return int(v * 255 / max), true
}

func colorFGBGLooksLight() bool {
	parts := strings.Split(os.Getenv("COLORFGBG"), ";")
	if len(parts) == 0 {
		return false
	}
	bg, err := strconv.Atoi(parts[len(parts)-1])
	return err == nil && (bg == 7 || bg == 15)
}

func fgSGR(c cliColor) string {
	if supportsTrueColor() && c.hex != "" {
		r, g, b, ok := parseHexColor(c.hex)
		if ok {
			return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
		}
	}
	return fmt.Sprintf("\033[38;5;%dm", c.xterm)
}

func bgSGR(c cliColor) string {
	if supportsTrueColor() && c.hex != "" {
		r, g, b, ok := parseHexColor(c.hex)
		if ok {
			return fmt.Sprintf("\033[48;2;%d;%d;%dm", r, g, b)
		}
	}
	return fmt.Sprintf("\033[48;5;%dm", c.xterm)
}

func parseHexColor(hex string) (int, int, int, bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, false
	}
	r, errR := strconv.ParseUint(hex[0:2], 16, 8)
	g, errG := strconv.ParseUint(hex[2:4], 16, 8)
	b, errB := strconv.ParseUint(hex[4:6], 16, 8)
	return int(r), int(g), int(b), errR == nil && errG == nil && errB == nil
}

func supportsTrueColor() bool {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	if strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit") {
		return true
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode":
		return true
	default:
		return false
	}
}

func themeFg(c cliColor, s string) string {
	return sgr(fgSGR(c), s)
}

func themeLipColor(c cliColor) color.Color {
	if supportsTrueColor() && c.hex != "" {
		return lipgloss.Color(c.hex)
	}
	return lipgloss.Color(strconv.Itoa(c.xterm))
}

func themeStyle(c cliColor) lipgloss.Style {
	if !colorEnabled {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(themeLipColor(c))
}

func withThemeFG(st lipgloss.Style, c cliColor) lipgloss.Style {
	if !colorEnabled {
		return st
	}
	return st.Foreground(themeLipColor(c))
}

func withThemeBorderFG(st lipgloss.Style, c cliColor) lipgloss.Style {
	if !colorEnabled {
		return st
	}
	return st.BorderForeground(themeLipColor(c))
}

func init() {
	refreshCLIStyles()
}

func refreshCLIStyles() {
	inputBoxStyle = withThemeBorderFG(lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, true, false), activeCLITheme.accent).
		PaddingLeft(1)
	approvalBannerStyle = withThemeFG(withThemeBorderFG(lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, true, false), activeCLITheme.warn), activeCLITheme.warn).
		Bold(true).
		PaddingLeft(1)
	todoPanelStyle = withThemeBorderFG(lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false), activeCLITheme.border).
		PaddingLeft(1)
	statusBlockStyle = themeStyle(activeCLITheme.faint)
	workingStyle = themeStyle(activeCLITheme.faint)
	compSelStyle = themeStyle(activeCLITheme.accent).Bold(true)
	choicePanelStyle = withThemeBorderFG(lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, true, false), activeCLITheme.accent).
		PaddingLeft(1)
	scrollThumbStyle = themeStyle(activeCLITheme.accent)
	scrollTrackStyle = themeStyle(activeCLITheme.faint)
}

func applyTextareaTheme(ti *textarea.Model) {
	plain := lipgloss.NewStyle()
	weak := themeStyle(activeCLITheme.faint)
	if !colorEnabled {
		weak = plain
	}

	styles := ti.Styles()
	styles.Focused = textarea.StyleState{
		Base:             plain,
		Text:             plain,
		CursorLine:       plain,
		CursorLineNumber: weak,
		EndOfBuffer:      weak,
		LineNumber:       weak,
		Placeholder:      weak,
		Prompt:           weak,
	}
	styles.Blurred = textarea.StyleState{
		Base:             plain,
		Text:             plain,
		CursorLine:       plain,
		CursorLineNumber: weak,
		EndOfBuffer:      weak,
		LineNumber:       weak,
		Placeholder:      weak,
		Prompt:           weak,
	}
	if colorEnabled {
		styles.Cursor.Color = themeLipColor(activeCLITheme.accent)
	} else {
		styles.Cursor.Color = nil
	}
	ti.SetStyles(styles)
}

func (m *chatTUI) runThemeSubcommand(input string) {
	args := tokenizeArgs(input)
	if len(args) < 2 {
		m.notice(i18n.M.ThemeHeader + "\n" + describeCLIThemes() + "\n" + i18n.M.ThemeHint)
		return
	}
	name := strings.ToLower(args[1])
	var theme cliPalette
	switch name {
	case "auto", "light", "dark":
		theme = setCLIThemeMode(name)
	default:
		next, ok := setCLIThemeStyle(name)
		if !ok {
			m.notice(fmt.Sprintf(i18n.M.ThemeUnknownFmt, name) + "\n" + describeCLIThemes())
			return
		}
		theme = next
	}
	m.refreshRuntimeTheme()
	m.notice(fmt.Sprintf(i18n.M.ThemeChangedFmt, theme.name, theme.style))

	// Persist to user config so the choice survives restart.
	m.persistTheme(name)
}

func (m *chatTUI) persistTheme(inputName string) {
	path := config.UserConfigPath()
	if path == "" {
		return
	}
	edit := config.LoadForEdit(path)
	switch inputName {
	case "auto", "light", "dark":
		edit.UI.Theme = inputName
		edit.UI.ThemeStyle = activeCLITheme.style
	default:
		edit.UI.Theme = activeCLITheme.name
		edit.UI.ThemeStyle = inputName
	}
	if err := edit.SaveTo(path); err != nil {
		slog.Warn("theme: failed to persist", "path", path, "err", err)
	}
}

func (m *chatTUI) refreshRuntimeTheme() {
	m.spinner.Style = themeStyle(activeCLITheme.accent)
	applyTextareaTheme(&m.input)
}

func describeCLIThemes() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  auto · light · dark\n", dim("modes:"))
	for _, st := range cliThemeStyles {
		marker := "  "
		if st.name == activeCLITheme.style {
			marker = accent("› ")
		}
		fmt.Fprintf(&b, "%s%-10s %s  %s\n", marker, st.name, dim(st.mode), dim(st.description))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *chatTUI) themeArgItems(val string) ([]compItem, int, bool) {
	cmdEnd := strings.IndexAny(val, " \t")
	if cmdEnd < 0 || val[:cmdEnd] != "/theme" {
		return nil, 0, false
	}
	from := strings.LastIndexAny(val, " \t") + 1
	prior := strings.Fields(val[:from])
	if len(prior) != 1 {
		return nil, from, true
	}
	cur := strings.ToLower(val[from:])
	items := []struct {
		label string
		mode  string
		desc  string
	}{
		{label: "auto", mode: "mode", desc: "detect terminal background"},
		{label: "light", mode: "mode", desc: "force light shell"},
		{label: "dark", mode: "mode", desc: "force dark shell"},
	}
	var out []compItem
	for _, it := range items {
		if cur != "" && !strings.HasPrefix(it.label, cur) {
			continue
		}
		out = append(out, compItem{label: it.label, insert: it.label, hint: it.mode + " · " + it.desc})
	}
	for _, st := range cliThemeStyles {
		if cur != "" && !strings.HasPrefix(st.name, cur) {
			continue
		}
		hint := st.mode + " · " + st.description
		if st.name == activeCLITheme.style {
			hint = i18n.M.ArgThemeCurrent
		}
		out = append(out, compItem{label: st.name, insert: st.name, hint: hint})
	}
	return out, from, true
}
