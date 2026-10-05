package termrender

import (
	"fmt"
	"strings"
)

// SetThemeMode switches between auto, light and dark, keeping the accent style
// when it belongs to the resulting mode.
func SetThemeMode(mode string) Palette {
	return setCLIThemeMode(mode)
}

// SetThemeStyle selects a named accent style, which also fixes the mode. It
// reports false for a name no style carries.
func SetThemeStyle(name string) (Palette, bool) {
	st, ok := cliThemeStyleByName(name)
	if !ok {
		return Palette{}, false
	}
	activeTheme = resolveCLIThemeWithStyle(st.mode, st.name)
	refreshCLIStyles()
	return activeTheme, true
}

// IsThemeMode reports whether name is one of the three modes rather than a style.
func IsThemeMode(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "auto", "light", "dark":
		return true
	}
	return false
}

// DescribeThemes lists the modes and every style, marking the active style.
func DescribeThemes() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  auto · light · dark\n", Dim("modes:"))
	for _, st := range cliThemeStyles {
		marker := "  "
		if st.name == activeTheme.Style {
			marker = Accent("› ")
		}
		fmt.Fprintf(&b, "%s%-10s %s  %s\n", marker, st.name, Dim(st.mode), Dim(st.description))
	}
	return strings.TrimRight(b.String(), "\n")
}
