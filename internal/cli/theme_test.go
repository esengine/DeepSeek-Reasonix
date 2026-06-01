package cli

import "testing"

func TestThemeArgCompletion(t *testing.T) {
	defer setCLITheme("graphite")
	setCLITheme("graphite")

	m := newTestChatTUI()
	items, _, ok := m.slashArgItems("/theme ")
	if !ok || len(items) == 0 {
		t.Fatalf("/theme arg completion should offer themes, ok=%v n=%d", ok, len(items))
	}
	if !hasLabel(items, "graphite") || !hasLabel(items, "aurora") {
		t.Fatalf("/theme completion missing expected themes: %v", labels(items))
	}
}

func TestRunThemeSubcommandSwitchesAccent(t *testing.T) {
	defer setCLITheme("graphite")
	setCLITheme("graphite")

	m := newTestChatTUI()
	m.runThemeSubcommand("/theme aurora")
	if currentCLITheme.Name != "aurora" {
		t.Fatalf("current theme = %q, want aurora", currentCLITheme.Name)
	}
	if ansiAccent != "\033[38;5;79m" {
		t.Fatalf("ansiAccent = %q, want aurora xterm color", ansiAccent)
	}
}
