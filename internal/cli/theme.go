package cli

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"reasonix/internal/i18n"
)

type cliTheme struct {
	Name        string
	Mode        string
	Color       string
	Description string
}

var cliThemes = []cliTheme{
	{Name: "graphite", Mode: "dark", Color: "173", Description: "warm clay accent"},
	{Name: "ember", Mode: "dark", Color: "209", Description: "hot orange accent"},
	{Name: "aurora", Mode: "dark", Color: "79", Description: "cool teal accent"},
	{Name: "midnight", Mode: "dark", Color: "141", Description: "quiet violet accent"},
	{Name: "sandstone", Mode: "light", Color: "173", Description: "default warm light accent"},
	{Name: "porcelain", Mode: "light", Color: "104", Description: "soft violet light accent"},
	{Name: "linen", Mode: "light", Color: "167", Description: "muted coral light accent"},
	{Name: "glacier", Mode: "light", Color: "74", Description: "cool blue light accent"},
}

var currentCLITheme = cliThemes[0]

func init() {
	applyCLITheme(currentCLITheme)
}

func cliThemeColor() color.Color {
	return lipgloss.Color(currentCLITheme.Color)
}

func setCLITheme(name string) (cliTheme, bool) {
	for _, theme := range cliThemes {
		if theme.Name == name {
			currentCLITheme = theme
			applyCLITheme(theme)
			return theme, true
		}
	}
	return cliTheme{}, false
}

func applyCLITheme(theme cliTheme) {
	color := lipgloss.Color(theme.Color)
	ansiAccent = "\033[38;5;" + theme.Color + "m"
	inputBoxStyle = inputBoxStyle.BorderForeground(color)
	compSelStyle = compSelStyle.Foreground(color)
	choicePanelStyle = choicePanelStyle.BorderForeground(color)
}

func (m *chatTUI) runThemeSubcommand(input string) {
	args := tokenizeArgs(input)
	if len(args) < 2 {
		m.notice(i18n.M.ThemeHeader + "\n" + describeCLIThemes() + "\n" + i18n.M.ThemeHint)
		return
	}
	name := strings.ToLower(args[1])
	theme, ok := setCLITheme(name)
	if !ok {
		m.notice(fmt.Sprintf(i18n.M.ThemeUnknownFmt, name) + "\n" + describeCLIThemes())
		return
	}
	m.spinner.Style = lipgloss.NewStyle().Foreground(cliThemeColor())
	m.notice(fmt.Sprintf(i18n.M.ThemeChangedFmt, theme.Name))
}

func describeCLIThemes() string {
	var b strings.Builder
	for _, theme := range cliThemes {
		marker := "  "
		if theme.Name == currentCLITheme.Name {
			marker = accent("› ")
		}
		fmt.Fprintf(&b, "%s%-10s %s  %s\n", marker, theme.Name, dim(theme.Mode), dim(theme.Description))
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
	var out []compItem
	for _, theme := range cliThemes {
		if cur != "" && !strings.HasPrefix(theme.Name, cur) {
			continue
		}
		hint := theme.Mode + " · " + theme.Description
		if theme.Name == currentCLITheme.Name {
			hint = i18n.M.ArgThemeCurrent
		}
		out = append(out, compItem{label: theme.Name, insert: theme.Name, hint: hint})
	}
	return out, from, true
}
