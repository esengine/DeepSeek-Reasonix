package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDesktopHistoryPageTurnsDefaultsAndValidates(t *testing.T) {
	if got := Default().DesktopHistoryPageTurns(); got != 60 {
		t.Fatalf("default history page turns = %d, want 60", got)
	}
	c := Default()
	for _, turns := range []int{1, 60, 200} {
		if err := c.SetDesktopHistoryPageTurns(turns); err != nil {
			t.Fatalf("SetDesktopHistoryPageTurns(%d): %v", turns, err)
		}
		if got := c.DesktopHistoryPageTurns(); got != turns {
			t.Fatalf("DesktopHistoryPageTurns() = %d, want %d", got, turns)
		}
	}
	for _, turns := range []int{-1, 0, 201} {
		before := c.DesktopHistoryPageTurns()
		if err := c.SetDesktopHistoryPageTurns(turns); err == nil {
			t.Fatalf("SetDesktopHistoryPageTurns(%d) succeeded", turns)
		}
		if got := c.DesktopHistoryPageTurns(); got != before {
			t.Fatalf("invalid setter changed value to %d, want %d", got, before)
		}
	}

	for _, invalid := range []int{-1, 201} {
		c.Desktop.HistoryPageTurns = invalid
		if got := c.DesktopHistoryPageTurns(); got != DefaultDesktopHistoryPageTurns {
			t.Fatalf("invalid loaded value %d normalized to %d, want %d", invalid, got, DefaultDesktopHistoryPageTurns)
		}
	}
}

func TestDesktopHistoryPageTurnsRenderRoundTrip(t *testing.T) {
	c := Default()
	if err := c.SetDesktopHistoryPageTurns(125); err != nil {
		t.Fatal(err)
	}
	rendered := RenderTOMLForScope(c, RenderScopeUser)
	if !strings.Contains(rendered, "history_page_turns = 125") {
		t.Fatalf("rendered config missing history_page_turns:\n%s", rendered)
	}
	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}
	if got.DesktopHistoryPageTurns() != 125 {
		t.Fatalf("round trip history page turns = %d, want 125", got.DesktopHistoryPageTurns())
	}
}
