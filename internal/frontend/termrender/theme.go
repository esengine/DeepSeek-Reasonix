package termrender

import (
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Color struct {
	hex string
	// Distance-based downsampling collapses the dark, low-chroma diff backgrounds
	// to plain grey and loses the red/green tint that carries their meaning, so
	// the 256-colour fallback stays hand-chosen rather than computed.
	xterm int
}

type Palette struct {
	Name         string
	Style        string
	Accent       Color
	Muted        Color
	Faint        Color
	Subtle       Color
	Success      Color
	Warn         Color
	Err          Color
	Danger       Color
	Info         Color
	Secondary    Color
	Border       Color
	Selection    Color
	UserBubbleBG Color
	DiffAddBG    Color
	DiffDelBG    Color
	ToolRead     Color
	ToolProc     Color
}

type cliThemeStyle struct {
	name        string
	mode        string
	accent      Color
	description string
}

var (
	cliDarkTheme = Palette{
		Name:         "dark",
		Style:        "graphite",
		Accent:       Color{"#d97757", 173},
		Muted:        Color{"#c0c4cc", 251},
		Faint:        Color{"#858b96", 245},
		Subtle:       Color{"#a4a9b3", 248},
		Success:      Color{"#74b87a", 108},
		Warn:         Color{"#d9a441", 179},
		Err:          Color{"#e0696a", 167},
		Danger:       Color{"#e5484d", 167},
		Info:         Color{"#56b6c2", 80},
		Secondary:    Color{"#b18cff", 141},
		Border:       Color{"#343945", 237},
		Selection:    Color{"#d97757", 173},
		UserBubbleBG: Color{"#222631", 235},
		DiffAddBG:    Color{"#14351d", 22},
		DiffDelBG:    Color{"#3a1619", 52},
		ToolRead:     Color{"#56b6c2", 80},
		ToolProc:     Color{"#c678dd", 176},
	}
	cliLightTheme = Palette{
		Name:         "light",
		Style:        "sandstone",
		Accent:       Color{"#2f5fa8", 25},
		Muted:        Color{"#555049", 239},
		Faint:        Color{"#82796f", 243},
		Subtle:       Color{"#6f675f", 241},
		Success:      Color{"#5d9b66", 65},
		Warn:         Color{"#b68120", 136},
		Err:          Color{"#b94b4d", 131},
		Danger:       Color{"#e5484d", 167},
		Info:         Color{"#2f5fa8", 25},
		Secondary:    Color{"#7d63c8", 104},
		Border:       Color{"#ded4c6", 252},
		Selection:    Color{"#6f91d9", 68},
		UserBubbleBG: Color{"#f5f0e8", 255},
		DiffAddBG:    Color{"#e5f3e7", 254},
		DiffDelBG:    Color{"#fae8e8", 255},
		ToolRead:     Color{"#6f91d9", 68},
		ToolProc:     Color{"#8a6bb8", 97},
	}
	cliThemeStyles = []cliThemeStyle{
		{name: "graphite", mode: "dark", accent: Color{"#d97757", 173}, description: "warm clay accent"},
		{name: "ember", mode: "dark", accent: Color{"#f06d38", 209}, description: "hot orange accent"},
		{name: "aurora", mode: "dark", accent: Color{"#34c3a6", 79}, description: "cool teal accent"},
		{name: "midnight", mode: "dark", accent: Color{"#b18cff", 141}, description: "quiet violet accent"},
		{name: "sandstone", mode: "light", accent: Color{"#c2613f", 173}, description: "default warm light accent"},
		{name: "porcelain", mode: "light", accent: Color{"#7d63c8", 104}, description: "soft violet light accent"},
		{name: "linen", mode: "light", accent: Color{"#bd5d4d", 167}, description: "muted coral light accent"},
		{name: "glacier", mode: "light", accent: Color{"#357fa8", 74}, description: "cool blue light accent"},
	}
	activeTheme = applyCLIThemeStyle(cliDarkTheme, cliThemeStyles[0])
	// activeBackgroundProbe stays inert unless a caller that owns stdin opts in
	// through withTerminalProbe; terminalProbe is what opting in installs.
	activeBackgroundProbe = noTerminalBackground
	terminalProbe         = queryTerminalBackground
)

// ActiveTheme returns the palette every styling helper currently draws with.
func ActiveTheme() Palette { return activeTheme }

// ThemeName is the active palette's mode: "dark" or "light".
func ThemeName() string { return activeTheme.Name }

func noTerminalBackground() (terminalRGB, bool) { return terminalRGB{}, false }

// cursorShape is the active cursor shape for the textarea input, configured
// via [ui] cursor_shape. Defaults to the slim bar used by the chat composer.
var cursorShape = "bar"

func ConfigureTheme(mode string) {
	configureThemeWithStyle(mode, "")
}

func configureThemeWithStyle(mode, style string) {
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
	activeTheme = resolveCLIThemeWithStyle(mode, style)
	refreshCLIStyles()
}

func resolveCLITheme(mode string) Palette {
	return resolveCLIThemeWithStyle(mode, "")
}

func resolveCLIThemeWithStyle(mode, style string) Palette {
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

func buildCLITheme(mode, style string) Palette {
	base := cliDarkTheme
	if mode == "light" {
		base = cliLightTheme
	}
	st, ok := cliThemeStyleByName(style)
	if !ok || st.mode != base.Name {
		st = defaultCLIThemeStyle(base.Name)
	}
	return applyCLIThemeStyle(base, st)
}

func applyCLIThemeStyle(base Palette, style cliThemeStyle) Palette {
	base.Style = style.name
	base.Accent = style.accent
	base.Selection = style.accent
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

func setCLIThemeMode(mode string) Palette {
	activeTheme = resolveCLIThemeWithStyle(mode, activeTheme.Style)
	refreshCLIStyles()
	return activeTheme
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
	_, after, ok := strings.Cut(s, "]11;")
	if !ok {
		return terminalRGB{}, false
	}
	payload := after
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
		if after, ok := strings.CutPrefix(payload, prefix); ok {
			return parseOSCColorTriplet(after)
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

func fgSGR(c Color) string {
	if trueColorTerminal() {
		if r, g, b, ok := parseHexColor(c.hex); ok {
			return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
		}
	}
	return fmt.Sprintf("\033[38;5;%dm", c.xterm)
}

func bgSGR(c Color) string {
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

func ThemeFg(c Color, s string) string {
	return sgr(fgSGR(c), s)
}

// NewColor is a color outside the palette, with its 256-color fallback.
func NewColor(hex string, xterm int) Color { return Color{hex, xterm} }

// Badge is s in bold on a solid background, padded one cell each side.
func Badge(bg, fg Color, s string) string {
	return sgr(bgSGR(bg)+fgSGR(fg)+ansiBold, " "+s+" ")
}

// ThemeLipColor pre-resolves the fallback rather than handing lipgloss a 24-bit
// value: the bubbletea renderer would otherwise downsample it with the same
// distance metric the hand-chosen xterm indices exist to avoid.
func ThemeLipColor(c Color) color.Color {
	if trueColorTerminal() {
		return lipgloss.Color(c.hex)
	}
	return lipgloss.Color(strconv.Itoa(c.xterm))
}

func ThemeStyle(c Color) lipgloss.Style {
	if !colorOn() {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(ThemeLipColor(c))
}

func init() {
	refreshCLIStyles()
}

func refreshCLIStyles() {
}

func ApplyTextareaTheme(ti *textarea.Model) {
	plain := lipgloss.NewStyle()
	weak := ThemeStyle(activeTheme.Faint)
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
		styles.Cursor.Color = ThemeLipColor(activeTheme.Accent)
	} else {
		styles.Cursor.Color = nil
	}
	switch cursorShape {
	case "block":
		styles.Cursor.Shape = tea.CursorBlock
	case "underline":
		styles.Cursor.Shape = tea.CursorUnderline
	default:
		styles.Cursor.Shape = tea.CursorBar
	}
	ti.SetStyles(styles)
}
