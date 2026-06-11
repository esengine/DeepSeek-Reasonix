package cli

import (
	"testing"
)

func TestFrameLines(t *testing.T) {
	tests := []struct {
		name      string
		filtered  int
		termRows  int
		searching bool
		wantLines int
	}{
		{
			name:      "small list no search",
			filtered:  5,
			termRows:  24,
			searching: false,
			wantLines: 4 + 5, // fixed(4) + items(5)
		},
		{
			name:      "small list with search",
			filtered:  5,
			termRows:  24,
			searching: true,
			wantLines: 5 + 5, // fixed(5 search) + items(5)
		},
		{
			name:      "list exceeds terminal height",
			filtered:  100,
			termRows:  24,
			searching: false,
			wantLines: 4 + 20, // fixed(4) + viewport(24-4=20)
		},
		{
			name:      "list exceeds terminal with search",
			filtered:  100,
			termRows:  24,
			searching: true,
			wantLines: 5 + 19, // fixed(5) + viewport(24-5=19)
		},
		{
			name:      "empty list",
			filtered:  0,
			termRows:  24,
			searching: false,
			wantLines: 4 + 0, // fixed(4) + viewport(0 items)
		},
		{
			name:      "tiny terminal",
			filtered:  10,
			termRows:  8,
			searching: false,
			wantLines: 4 + 4, // fixed(4) + viewport(8-4=4)
		},
		{
			name:      "tiny terminal with search",
			filtered:  10,
			termRows:  8,
			searching: true,
			wantLines: 5 + 3, // fixed(5) + viewport(8-5=3)
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FrameLines(tt.filtered, tt.termRows, tt.searching)
			if got != tt.wantLines {
				t.Errorf("FrameLines(%d, %d, %v) = %d, want %d",
					tt.filtered, tt.termRows, tt.searching, got, tt.wantLines)
			}
		})
	}
}

func TestFrameLinesNeverExceedsTerminal(t *testing.T) {
	// The frame must never print more lines than the terminal has rows.
	// Otherwise the terminal scrolls and cursor repositioning drifts.
	termRows := 24
	for _, searching := range []bool{false, true} {
		for n := 0; n <= 200; n++ {
			lines := FrameLines(n, termRows, searching)
			if lines > termRows {
				t.Errorf("FrameLines(%d, %d, searching=%v) = %d, exceeds terminal",
					n, termRows, searching, lines)
			}
		}
	}
}

func TestMaxViewportBounds(t *testing.T) {
	// Viewport must be at least 1 even on impossibly small terminals.
	vp := maxViewport(10, 3, false)
	if vp < 1 {
		t.Errorf("maxViewport(10, 3, false) = %d, want >= 1", vp)
	}
	// When items < available rows, viewport equals items.
	vp = maxViewport(3, 24, false)
	if vp != 3 {
		t.Errorf("maxViewport(3, 24, false) = %d, want 3", vp)
	}
}

func TestFilterMenuItems(t *testing.T) {
	items := []menuItem{
		{name: "deepseek-v4", desc: "DeepSeek V4"},
		{name: "gpt-4o", desc: "OpenAI GPT-4o"},
		{name: "mimo-pro", desc: "MiMo Pro"},
	}
	// Empty query returns all.
	if got := filterMenuItems(items, ""); len(got) != 3 {
		t.Errorf("empty query: got %d, want 3", len(got))
	}
	// Case-insensitive match on name.
	if got := filterMenuItems(items, "GPT"); len(got) != 1 || got[0].name != "gpt-4o" {
		t.Errorf("GPT: got %v", got)
	}
	// Case-insensitive match on desc.
	if got := filterMenuItems(items, "mimo"); len(got) != 1 || got[0].name != "mimo-pro" {
		t.Errorf("mimo: got %v", got)
	}
	// No match.
	if got := filterMenuItems(items, "claude"); len(got) != 0 {
		t.Errorf("claude: got %d, want 0", len(got))
	}
}
