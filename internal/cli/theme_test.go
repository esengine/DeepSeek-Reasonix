package cli

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestConfigureCLIThemeSwitchesModeAndDefaultStyle(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	configureCLITheme("light")
	if activeCLITheme.name != "light" || activeCLITheme.style != "sandstone" {
		t.Fatalf("light theme = %s/%s, want light/sandstone", activeCLITheme.name, activeCLITheme.style)
	}
	if got := accent("x"); !strings.HasPrefix(got, "\033[38;5;173m") {
		t.Fatalf("light default accent = %q, want sandstone xterm 173", got)
	}

	configureCLITheme("dark")
	if activeCLITheme.name != "dark" || activeCLITheme.style != "graphite" {
		t.Fatalf("dark theme = %s/%s, want dark/graphite", activeCLITheme.name, activeCLITheme.style)
	}
	if got := accent("x"); !strings.HasPrefix(got, ansiAccent) {
		t.Fatalf("dark accent = %q, want %q", got, ansiAccent)
	}
}

func TestConfigureCLIThemeStyleOverride(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	configureCLIThemeWithStyle("dark", "aurora")
	if activeCLITheme.name != "dark" || activeCLITheme.style != "aurora" {
		t.Fatalf("theme = %s/%s, want dark/aurora", activeCLITheme.name, activeCLITheme.style)
	}
	if got := accent("x"); !strings.HasPrefix(got, "\033[38;5;79m") {
		t.Fatalf("aurora accent = %q, want xterm 79", got)
	}

	configureCLITheme("glacier")
	if activeCLITheme.name != "light" || activeCLITheme.style != "glacier" {
		t.Fatalf("theme style command resolved %s/%s, want light/glacier", activeCLITheme.name, activeCLITheme.style)
	}
}

func TestConfigureCLIThemeHonorsEnvOverride(t *testing.T) {
	t.Setenv("REASONIX_THEME", "ember")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	configureCLIThemeWithStyle("light", "glacier")
	if activeCLITheme.name != "dark" || activeCLITheme.style != "ember" {
		t.Fatalf("REASONIX_THEME override resolved %s/%s, want dark/ember", activeCLITheme.name, activeCLITheme.style)
	}
}

func TestThemeRendersAtProfileFidelity(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	configureCLIThemeWithStyle("dark", "graphite")

	activeColorProfile = colorprofile.TrueColor
	if got := accent("x"); !strings.HasPrefix(got, "\033[38;2;217;119;87m") {
		t.Fatalf("truecolor accent = %q, want 24-bit #d97757", got)
	}

	activeColorProfile = colorprofile.ANSI256
	if got := accent("x"); !strings.HasPrefix(got, ansiAccent) {
		t.Fatalf("256-colour accent = %q, want %q", got, ansiAccent)
	}

	activeColorProfile = colorprofile.NoTTY
	if got := accent("x"); got != "x" {
		t.Fatalf("no-tty accent = %q, want unstyled text", got)
	}
}

func TestThemeArgCompletion(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLIThemeWithStyle("dark", "graphite")

	m := newTestChatTUI()
	items, _, ok := m.slashArgItems("/theme ")
	if !ok || len(items) == 0 {
		t.Fatalf("/theme arg completion should offer themes, ok=%v n=%d", ok, len(items))
	}
	if !hasLabel(items, "auto") || !hasLabel(items, "graphite") || !hasLabel(items, "aurora") {
		t.Fatalf("/theme completion missing expected themes: %v", labels(items))
	}
}

func TestRunThemeSubcommandSwitchesAccentAndTextarea(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLIThemeWithStyle("dark", "graphite")

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	if cmd := m.runThemeSubcommand("/theme aurora"); cmd == nil {
		t.Fatal("a real theme change should start the sweep")
	}
	if activeCLITheme.name != "dark" || activeCLITheme.style != "aurora" {
		t.Fatalf("current theme = %s/%s, want dark/aurora", activeCLITheme.name, activeCLITheme.style)
	}
	if got := accent("x"); !strings.HasPrefix(got, "\033[38;5;79m") {
		t.Fatalf("accent = %q, want aurora xterm color", got)
	}
	if m.input.Styles().Cursor.Color == nil {
		t.Fatal("textarea cursor color was not refreshed")
	}
}

func TestThemePickerPreviewLifecycle(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.NoTTY} {
		for _, action := range []string{"confirm", "escape", "clear", "no matches", "other command", "tab"} {
			t.Run(profile.String()+"/"+action, func(t *testing.T) {
				t.Setenv("REASONIX_HOME", t.TempDir())
				t.Setenv("REASONIX_THEME", "")
				t.Setenv("REASONIX_THEME_STYLE", "")
				defer restoreThemeForTest(activeColorProfile, activeCLITheme)
				activeColorProfile = profile
				configureCLITheme("midnight")
				original := activeCLITheme
				m := newTestChatTUI()
				m.ctrl = control.New(control.Options{})
				m.started = true
				update := func(msg tea.Msg) {
					next, _ := m.Update(msg)
					m = next.(chatTUI)
				}
				update(tea.WindowSizeMsg{Width: 80, Height: 60})
				m.persistTheme("auto")
				configPath := config.UserConfigPath()
				saved, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				m.commitTranscriptSource(transcriptSource{kind: transcriptSourceUser, raw: "change this code"})
				m.commitTranscriptSource(transcriptSource{kind: transcriptSourceMarkdown, raw: "**An answer** with `code`."})
				m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
					Name: "write_file", Args: `{"path":"main.go"}`,
					FileDiff: event.FileDiff{Diff: "@@ -1 +1 @@\n-old\n+new\n", Added: 1, Removed: 1},
				}})
				m.beginToolRunning("shell-test")
				m.streamToolOutput("shell-test", "output\n")
				m.collapseToolOutput("shell-test", "output\n")
				m.notice("existing notice")
				m.refreshRuntimeTheme()
				before := slices.Clone(m.transcript)

				// Exercise the actual bare-command Enter path into the submenu.
				m.input.SetValue("/theme")
				m.input.CursorEnd()
				m.updateCompletion()
				update(tea.KeyPressMsg{Code: tea.KeyEnter})
				if m.selectedTheme() != "midnight" || m.themePreview == nil {
					t.Fatalf("picker did not open on the current theme: %+v", m.completion)
				}
				for range 4 {
					update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
				}
				if m.selectedTheme() != "glacier" || activeCLITheme.style != "glacier" {
					t.Fatalf("highlight was not previewed: %q / %q", m.selectedTheme(), activeCLITheme.style)
				}
				if colorOn() {
					if reflect.DeepEqual(before, m.transcript) || !strings.Contains(m.View().Content, bgSGR(activeCLITheme.diffAddBG)) {
						t.Fatal("preview did not repaint existing transcript and diff")
					}
				}
				if ansi.Strip(strings.Join(before, "\n")) != ansi.Strip(strings.Join(m.transcript, "\n")) {
					t.Fatal("preview changed transcript text")
				}
				if now, err := os.ReadFile(configPath); err != nil || string(now) != string(saved) {
					t.Fatalf("preview wrote configuration: %v", err)
				}

				switch action {
				case "confirm":
					update(tea.KeyPressMsg{Code: tea.KeyEnter})
				case "escape":
					update(tea.KeyPressMsg{Code: tea.KeyEscape})
				case "clear":
					update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				case "no matches":
					update(tea.KeyPressMsg{Code: 'x', Text: "x"})
				case "other command":
					m.input.SetValue("/language ")
					m.input.CursorEnd()
					m.updateCompletion()
					update(tea.FocusMsg{})
				case "tab":
					update(tea.KeyPressMsg{Code: tea.KeyTab})
				}
				update(themeSweepTickMsg{}) // an old animation tick must not revive the preview
				if m.themePreview != nil || m.themeSweep != nil {
					t.Fatal("picker left preview or animation state behind")
				}
				if action == "confirm" {
					cfg := config.LoadForEdit(configPath)
					if cfg.UI.Theme != "light" || cfg.UI.ThemeStyle != "glacier" || activeCLITheme.style != "glacier" {
						t.Fatalf("confirmation did not keep and save glacier: %+v", cfg.UI)
					}
					if len(m.transcript) != len(before)+1 {
						t.Fatal("confirmation should emit exactly one notice")
					}
				} else {
					if activeCLITheme != original || !reflect.DeepEqual(m.transcript, before) {
						t.Fatal("cancel did not restore the original theme and transcript")
					}
					if now, err := os.ReadFile(configPath); err != nil || string(now) != string(saved) {
						t.Fatalf("cancel changed configuration: %v", err)
					}
				}
			})
		}
	}
}

func TestThemePickerModeAliasesUseOriginalStyle(t *testing.T) {
	for _, name := range []string{"dark", "auto"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("REASONIX_HOME", t.TempDir())
			t.Setenv("REASONIX_THEME", "")
			t.Setenv("REASONIX_THEME_STYLE", "")
			t.Setenv("COLORFGBG", "15;0")
			defer restoreThemeForTest(activeColorProfile, activeCLITheme)
			configureCLITheme("midnight")
			m := newTestChatTUI()
			m.ctrl = control.New(control.Options{})
			for _, input := range []string{"/theme light", "/theme " + name} {
				m.input.SetValue(input)
				m.input.CursorEnd()
				m.updateCompletion()
				next, _ := m.Update(tea.FocusMsg{})
				m = next.(chatTUI)
			}
			if activeCLITheme.style != "midnight" {
				t.Fatalf("%s used the last preview's style instead of the original", name)
			}
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(chatTUI)
			cfg := config.LoadForEdit(config.UserConfigPath())
			if cfg.UI.Theme != name || cfg.UI.ThemeStyle != "midnight" || m.themePreview != nil {
				t.Fatalf("mode alias was not saved correctly: %+v", cfg.UI)
			}
		})
	}
}

func TestThemePickerResizeAndCancelDuringTurn(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "viewport", true: "native scrollback"}[native], func(t *testing.T) {
			t.Setenv("REASONIX_THEME", "")
			t.Setenv("REASONIX_THEME_STYLE", "")
			defer restoreThemeForTest(activeColorProfile, activeCLITheme)
			activeColorProfile = colorprofile.ANSI256
			configureCLITheme("midnight")
			original := activeCLITheme
			m := newTestChatTUI()
			m.ctrl = control.New(control.Options{})
			m.started, m.nativeScrollback = true, native
			m.state = tuiRunning
			m.spinner = spinner.New()
			update := func(msg tea.Msg) {
				next, _ := m.Update(msg)
				m = next.(chatTUI)
			}
			update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m.commitTranscriptSource(transcriptSource{kind: transcriptSourceMarkdown, raw: strings.Repeat("old line\n\n", 30)})
			m.refreshRuntimeTheme()
			m.viewport.SetYOffset(5)
			m.markUserScrolled()
			m.input.SetValue("/theme glacier")
			m.input.CursorEnd()
			m.updateCompletion()
			update(tea.FocusMsg{})
			if activeCLITheme.style != "glacier" || m.viewport.YOffset() != 5 {
				t.Fatal("preview did not apply or moved the scroll position")
			}
			// A streamed block added during preview must also be restored on cancel.
			m.notice("arrived during preview")
			update(tea.WindowSizeMsg{Width: 20, Height: 24})
			if activeCLITheme.style != "glacier" || m.themeSweep != nil {
				t.Fatal("resize lost the preview or retained a stale animation")
			}
			update(tea.KeyPressMsg{Code: tea.KeyEscape})
			if activeCLITheme != original || m.themePreview != nil || m.state != tuiRunning {
				t.Fatal("Escape must restore the theme without canceling the active turn")
			}
			if !strings.Contains(m.transcript[len(m.transcript)-1], fgSGR(original.faint)) {
				t.Fatal("new output retained the canceled preview colors")
			}
		})
	}
}

func TestParseOSC11Response(t *testing.T) {
	for _, tt := range []struct {
		name  string
		in    string
		want  terminalRGB
		light bool
	}{
		{
			name:  "black-rgb",
			in:    "\x1b]11;rgb:0000/0000/0000\a",
			want:  terminalRGB{0, 0, 0},
			light: false,
		},
		{
			name:  "white-rgb",
			in:    "\x1b]11;rgb:ffff/ffff/ffff\x1b\\",
			want:  terminalRGB{255, 255, 255},
			light: true,
		},
		{
			name:  "hex",
			in:    "\x1b]11;#f8f8f8\a",
			want:  terminalRGB{248, 248, 248},
			light: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseOSC11Response(tt.in)
			if !ok {
				t.Fatalf("parseOSC11Response returned !ok")
			}
			if got != tt.want {
				t.Fatalf("rgb = %+v, want %+v", got, tt.want)
			}
			if got.looksLight() != tt.light {
				t.Fatalf("looksLight = %v, want %v", got.looksLight(), tt.light)
			}
		})
	}
}

func TestAutoThemeFallsBackToColorFGBG(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY

	t.Setenv("COLORFGBG", "0;15")
	if got := resolveCLITheme("auto").name; got != "light" {
		t.Fatalf("COLORFGBG light fallback resolved %q, want light", got)
	}

	t.Setenv("COLORFGBG", "15;0")
	if got := resolveCLITheme("auto").name; got != "dark" {
		t.Fatalf("COLORFGBG dark fallback resolved %q, want dark", got)
	}
}

func TestApplyTextareaThemeClearsCursorLineBackground(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	for _, mode := range []string{"dark", "light", "auto"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "auto" {
				t.Setenv("COLORFGBG", "0;15")
			} else {
				t.Setenv("COLORFGBG", "")
			}
			configureCLITheme(mode)

			ti := textarea.New()
			applyTextareaTheme(&ti)
			styles := ti.Styles()
			emptyBG := lipgloss.NewStyle().GetBackground()

			if bg := styles.Focused.CursorLine.GetBackground(); !reflect.DeepEqual(bg, emptyBG) {
				t.Fatalf("focused cursor line background = %v, want empty", bg)
			}
			if bg := styles.Blurred.CursorLine.GetBackground(); !reflect.DeepEqual(bg, emptyBG) {
				t.Fatalf("blurred cursor line background = %v, want empty", bg)
			}
			if bg := styles.Focused.EndOfBuffer.GetBackground(); !reflect.DeepEqual(bg, emptyBG) {
				t.Fatalf("end-of-buffer background = %v, want empty", bg)
			}
			if styles.Cursor.Color == nil {
				t.Fatal("cursor color is nil with color enabled")
			}
		})
	}
}

func TestApplyTextareaThemeHonorsCursorShape(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	prevShape := cliCursorShape
	defer func() { cliCursorShape = prevShape }()
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")

	for _, tt := range []struct {
		name string
		in   string
		want tea.CursorShape
	}{
		{name: "default", in: "", want: tea.CursorBar},
		{name: "underline", in: "underline", want: tea.CursorUnderline},
		{name: "block", in: "block", want: tea.CursorBlock},
		{name: "bar", in: "bar", want: tea.CursorBar},
		{name: "unknown", in: "unknown", want: tea.CursorBar},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cliCursorShape = tt.in
			ti := textarea.New()
			applyTextareaTheme(&ti)
			if got := ti.Styles().Cursor.Shape; got != tt.want {
				t.Fatalf("cursor shape = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComposerBorderAndCursorTrackThemeAccent(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	for _, theme := range cliThemeStyles {
		t.Run(theme.name, func(t *testing.T) {
			configureCLITheme(theme.name)
			want := themeLipColor(activeCLITheme.accent)
			if got := inputBoxStyle.GetBorderTopForeground(); !reflect.DeepEqual(got, want) {
				t.Fatalf("composer top border color = %v, want theme accent %v", got, want)
			}
			if got := inputBoxStyle.GetBorderBottomForeground(); !reflect.DeepEqual(got, want) {
				t.Fatalf("composer bottom border color = %v, want theme accent %v", got, want)
			}

			ti := textarea.New()
			applyTextareaTheme(&ti)
			if got := ti.Styles().Cursor.Color; !reflect.DeepEqual(got, want) {
				t.Fatalf("composer cursor color = %v, want theme accent %v", got, want)
			}
		})
	}

	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")
	empty := lipgloss.NewStyle().GetBorderTopForeground()
	if got := inputBoxStyle.GetBorderTopForeground(); !reflect.DeepEqual(got, empty) {
		t.Fatalf("NO_COLOR composer top border color = %v, want no color", got)
	}
	if got := inputBoxStyle.GetBorderBottomForeground(); !reflect.DeepEqual(got, empty) {
		t.Fatalf("NO_COLOR composer bottom border color = %v, want no color", got)
	}
}

// TestRuntimeAutoThemeDoesNotProbeStdin guards the fix for a runtime `/theme auto`
// that live-probed the terminal (raw-mode stdin read) while the TUI owned stdin,
// racing bubbletea's input reader. The switch must resolve via the COLORFGBG
// fallback instead, never invoking the probe.
func TestRuntimeAutoThemeDoesNotProbeStdin(t *testing.T) {
	t.Setenv("REASONIX_THEME", "")
	t.Setenv("REASONIX_THEME_STYLE", "")
	t.Setenv("COLORFGBG", "15;0")
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256

	probed := false
	defer func(prev func() (terminalRGB, bool)) { terminalProbe = prev }(terminalProbe)
	terminalProbe = func() (terminalRGB, bool) {
		probed = true
		return terminalRGB{255, 255, 255}, true
	}

	if got := setCLIThemeMode("auto").name; got != "dark" {
		t.Fatalf("auto with COLORFGBG=15;0 resolved %q, want dark", got)
	}
	if probed {
		t.Fatal("runtime /theme auto probed the terminal while the TUI owns stdin")
	}

	withTerminalProbe(func() {
		if got := resolveCLITheme("auto").name; got != "light" {
			t.Fatalf("opted-in probe resolved %q, want light", got)
		}
	})
	if !probed {
		t.Fatal("withTerminalProbe should be the one path that reaches the terminal")
	}
}

func restoreThemeForTest(prevColor colorprofile.Profile, prevTheme cliPalette) {
	activeColorProfile = prevColor
	activeCLITheme = prevTheme
	refreshCLIStyles()
}
