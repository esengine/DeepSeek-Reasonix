package cli

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

type cliColor struct {
	hex string
	// Distance-based downsampling collapses the dark, low-chroma diff backgrounds
	// to plain grey and loses the red/green tint that carries their meaning, so
	// the 256-colour fallback stays hand-chosen rather than computed.
	xterm int
}

type cliPalette struct {
	name         string
	style        string
	accent       cliColor
	muted        cliColor
	faint        cliColor
	subtle       cliColor
	success      cliColor
	warn         cliColor
	err          cliColor
	danger       cliColor
	info         cliColor
	secondary    cliColor
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
	// Graphite: the monochrome default. Neutrals walk a cool grey ramp and every
	// semantic colour is desaturated toward it, so the surface reads as one flat
	// grey wash with only the faintest tinted signals for meaning.
	cliDarkTheme = cliPalette{
		name:         "dark",
		style:        "graphite",
		accent:       cliColor{"#9aa3b2", 247},
		muted:        cliColor{"#b8bcc6", 250},
		faint:        cliColor{"#767b87", 243},
		subtle:       cliColor{"#8f95a1", 245},
		success:      cliColor{"#8fbc98", 108},
		warn:         cliColor{"#c9b383", 179},
		err:          cliColor{"#cc8f8b", 174},
		danger:       cliColor{"#d9807a", 167},
		info:         cliColor{"#8fabb8", 109},
		secondary:    cliColor{"#a9a1c4", 146},
		border:       cliColor{"#2a2d35", 236},
		selection:    cliColor{"#9aa3b2", 247},
		userBubbleBG: cliColor{"#16181d", 234},
		diffAddBG:    cliColor{"#1a2e21", 22},
		diffDelBG:    cliColor{"#34211f", 52},
		toolRead:     cliColor{"#8fabb8", 109},
		toolProc:     cliColor{"#b3a8cc", 146},
	}
	cliLightTheme = cliPalette{
		name:         "light",
		style:        "sandstone",
		accent:       cliColor{"#4a515c", 240},
		muted:        cliColor{"#4d5159", 239},
		faint:        cliColor{"#838792", 244},
		subtle:       cliColor{"#686d77", 242},
		success:      cliColor{"#3f7a52", 65},
		warn:         cliColor{"#8a6f33", 137},
		err:          cliColor{"#a8524e", 131},
		danger:       cliColor{"#b04842", 167},
		info:         cliColor{"#3f7485", 30},
		secondary:    cliColor{"#6f6596", 104},
		border:       cliColor{"#d6d8dd", 253},
		selection:    cliColor{"#4a515c", 240},
		userBubbleBG: cliColor{"#e9eaed", 255},
		diffAddBG:    cliColor{"#e3efe6", 194},
		diffDelBG:    cliColor{"#f7e7e5", 255},
		toolRead:     cliColor{"#3f7485", 30},
		toolProc:     cliColor{"#7a6fa6", 97},
	}
	cliThemeStyles = []cliThemeStyle{
		// graphite/sandstone are the defaults and wear the monochrome graphite
		// accent; the remaining styles stay as optional personality accents for
		// users who opt in via /theme.
		{name: "graphite", mode: "dark", accent: cliColor{"#9aa3b2", 247}, description: "graphite gray accent"},
		{name: "ember", mode: "dark", accent: cliColor{"#f5832e", 209}, description: "hot ember accent"},
		{name: "aurora", mode: "dark", accent: cliColor{"#3ecfae", 79}, description: "cool teal accent"},
		{name: "midnight", mode: "dark", accent: cliColor{"#b494f5", 141}, description: "quiet violet accent"},
		{name: "sandstone", mode: "light", accent: cliColor{"#4a515c", 240}, description: "graphite gray accent"},
		{name: "porcelain", mode: "light", accent: cliColor{"#7c5cc8", 104}, description: "soft violet light accent"},
		{name: "linen", mode: "light", accent: cliColor{"#c25544", 167}, description: "muted coral light accent"},
		{name: "glacier", mode: "light", accent: cliColor{"#2e7fa8", 67}, description: "cool blue light accent"},
	}
	activeCLITheme = applyCLIThemeStyle(cliDarkTheme, cliThemeStyles[0])
	// activeBackgroundProbe stays inert unless a caller that owns stdin opts in
	// through withTerminalProbe; terminalProbe is what opting in installs.
	activeBackgroundProbe = noTerminalBackground
	terminalProbe         = queryTerminalBackground
)

func noTerminalBackground() (terminalRGB, bool) { return terminalRGB{}, false }

// cliCursorShape is the active cursor shape for the textarea input, configured
// via [ui] cursor_shape. Defaults to the slim bar used by the chat composer.
var cliCursorShape = "bar"

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
		if rgb, ok := activeBackgroundProbe(); ok {
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

// withTerminalProbe resolves "auto" against a live OSC 11 query. Probing reads
// stdin in raw mode, so only a caller that owns stdin may opt in; everyone else
// gets the COLORFGBG fallback and never fights the TUI's input reader.
func withTerminalProbe(fn func()) {
	prev := activeBackgroundProbe
	activeBackgroundProbe = terminalProbe
	defer func() { activeBackgroundProbe = prev }()
	fn()
}

func setCLIThemeMode(mode string) cliPalette {
	activeCLITheme = resolveCLIThemeWithStyle(mode, activeCLITheme.style)
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
	if trueColorTerminal() {
		if r, g, b, ok := parseHexColor(c.hex); ok {
			return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
		}
	}
	return fmt.Sprintf("\033[38;5;%dm", c.xterm)
}

func bgSGR(c cliColor) string {
	if trueColorTerminal() {
		if r, g, b, ok := parseHexColor(c.hex); ok {
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

func themeFg(c cliColor, s string) string {
	return sgr(fgSGR(c), s)
}

// themeLipColor pre-resolves the fallback rather than handing lipgloss a 24-bit
// value: the bubbletea renderer would otherwise downsample it with the same
// distance metric the hand-chosen xterm indices exist to avoid.
func themeLipColor(c cliColor) color.Color {
	if trueColorTerminal() {
		return lipgloss.Color(c.hex)
	}
	return lipgloss.Color(strconv.Itoa(c.xterm))
}

func themeStyle(c cliColor) lipgloss.Style {
	if !colorOn() {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(themeLipColor(c))
}

func withThemeBorderFG(st lipgloss.Style, c cliColor) lipgloss.Style {
	if !colorOn() {
		return st
	}
	return st.BorderForeground(themeLipColor(c))
}

func modeTagStyle(c cliColor) lipgloss.Style {
	// Flat mode label: bold text in the mode's semantic colour with a "●"
	// signal dot, no pill background — the monochrome default keeps the bar
	// quiet and the colour only carries the mode's meaning.
	st := lipgloss.NewStyle().Bold(true)
	if !colorOn() {
		return st
	}
	return st.Foreground(themeLipColor(c))
}

func init() {
	refreshCLIStyles()
}

// approvalPanelStyle frames the approval card in warn amber (see
// refreshCLIStyles). Declared here rather than next to the ask-card's
// choicePanelStyle because the shell renders it from chat_tui.go.
var approvalPanelStyle lipgloss.Style

func refreshCLIStyles() {
	// Mode tag colours follow the theme: plan wears the signature accent, auto
	// the warn amber, yolo danger, shell success (no hardcoded mode palette).
	statusAutoColor = activeCLITheme.warn
	statusPlanColor = activeCLITheme.accent
	statusYoloColor = activeCLITheme.danger
	statusShellColor = activeCLITheme.success
	// The composer is a full rounded container. Its border stays quiet (theme
	// border); the accent is reserved for the running state — View() applies
	// it via withThemeBorderFG — and for the cursor.
	inputBoxStyle = withThemeBorderFG(lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), true, true, true, true), activeCLITheme.border).
		PaddingLeft(1)
	todoPanelStyle = withThemeBorderFG(lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false), activeCLITheme.border).
		PaddingLeft(1)
	// Right-hand task-list column on wide terminals: an open divider beside the
	// chat column — vertical rails only, no top/bottom frame, so the compact
	// column never reads as a second bordered window.
	todoSidebarStyle = withThemeBorderFG(lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, true, false, true), activeCLITheme.border).
		PaddingLeft(1)
	// Status bar: flat faint text on the chat surface — no tinted band, so the
	// footer reads as content, not chrome. The hairline divider (statusFooterDivider)
	// separates the two information rows instead.
	statusBlockStyle = themeStyle(activeCLITheme.faint)
	workingStyle = themeStyle(activeCLITheme.faint)
	// Menu selection is a reverse accent chip: accent background with an ink
	// foreground (dark ink on the light graphite, near-white on dark graphite).
	compSelStyle = lipgloss.NewStyle().Reverse(true).Bold(true)
	if colorOn() {
		selFg := modeTagDark
		if activeCLITheme.name == "light" {
			selFg = modeTagLight
		}
		compSelStyle = lipgloss.NewStyle().
			Background(themeLipColor(activeCLITheme.accent)).
			Foreground(themeLipColor(selFg)).
			Bold(true)
	}
	// Bottom-sheet picker rows sit on the user-bubble surface, one step
	// lighter than the chat background, so the pop-up reads as an overlay.
	pickerSheetStyle = lipgloss.NewStyle().
		Background(themeLipColor(activeCLITheme.userBubbleBG))
	// User turns in the transcript wear the same surface as a solid band, so
	// they read as a block next to the assistant's plain flowing text.
	userBubbleStyle = lipgloss.NewStyle().
		Background(themeLipColor(activeCLITheme.userBubbleBG))
	choicePanelStyle = withThemeBorderFG(lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, true, false), activeCLITheme.border).
		PaddingLeft(1)
	// Approval cards wear the warn frame: "action needed" reads amber while
	// the accent frame stays with ask-cards.
	approvalPanelStyle = withThemeBorderFG(lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, true, false), activeCLITheme.warn).
		PaddingLeft(1)
	scrollThumbStyle = themeStyle(activeCLITheme.accent)
	scrollTrackStyle = themeStyle(activeCLITheme.faint)
}

func applyTextareaTheme(ti *textarea.Model) {
	plain := lipgloss.NewStyle()
	weak := themeStyle(activeCLITheme.faint)
	if !colorOn() {
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
	if colorOn() {
		styles.Cursor.Color = themeLipColor(activeCLITheme.accent)
	} else {
		styles.Cursor.Color = nil
	}
	switch cliCursorShape {
	case "block":
		styles.Cursor.Shape = tea.CursorBlock
	case "underline":
		styles.Cursor.Shape = tea.CursorUnderline
	default:
		styles.Cursor.Shape = tea.CursorBar
	}
	ti.SetStyles(styles)
}

func (m *chatTUI) runThemeSubcommand(input string) tea.Cmd {
	args := tokenizeArgs(input)
	if len(args) < 2 {
		m.notice(i18n.M.ThemeHeader + "\n" + describeCLIThemes() + "\n" + i18n.M.ThemeHint)
		return nil
	}
	name := strings.ToLower(args[1])
	previous := activeCLITheme
	var theme cliPalette
	switch name {
	case "auto", "light", "dark":
		theme = setCLIThemeMode(name)
	default:
		next, ok := setCLIThemeStyle(name)
		if !ok {
			m.notice(fmt.Sprintf(i18n.M.ThemeUnknownFmt, name) + "\n" + describeCLIThemes())
			return nil
		}
		theme = next
	}
	m.refreshRuntimeTheme()
	m.notice(fmt.Sprintf(i18n.M.ThemeChangedFmt, theme.name, theme.style))

	// Persist to user config so the choice survives restart.
	m.persistTheme(name)
	return m.startThemeSweep(previous, theme)
}

func (m *chatTUI) persistTheme(inputName string) {
	path := config.UserConfigPath()
	if path == "" {
		return
	}
	// Serialize the load-modify-save against other in-process user-config
	// editors so concurrent writers don't drop each other's fields.
	unlock := config.LockUserConfigEdits()
	defer unlock()
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
