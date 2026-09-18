package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
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
		for n := range 201 {
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

// A frame line that soft-wraps occupies more rows than redraw counts, so a
// narrow terminal used to stack a fresh copy of the header and the wrapped
// rows on every keypress. Every line the frame emits must fit its width.
func TestMenuFrameLinesFitTerminalWidth(t *testing.T) {
	prev := activeColorProfile
	activeColorProfile = colorprofile.ANSI256
	t.Cleanup(func() { activeColorProfile = prev })

	items := []menuItem{
		{name: "deepseek-pro", desc: "openai · 1 models · key missing"},
		{name: "Add Anthropic-compatible provider", desc: "Add third-party Anthropic compatible model"},
		{name: "中转站", desc: "自定义 OpenAI 兼容模型 · 密钥缺失"},
	}
	for _, cols := range []int{1, 8, 40, 60} {
		var buf bytes.Buffer
		f := menuFrame{w: &buf, cols: cols}
		f.header("Provider configuration", "(↑/↓ · Enter · q to cancel; / to search)")
		f.searchBar("a query that is much longer than the narrowest terminal")
		for i, it := range items {
			f.row(i == 0, fmt.Sprintf("%-10s", it.name), it.desc)
		}
		out := buf.String()
		if strings.Count(out, "\n") != strings.Count(out, "\r\n") {
			t.Errorf("cols=%d: a line ends with a bare LF, which leaves the cursor mid-row in raw mode", cols)
		}
		lines := strings.Split(strings.TrimSuffix(out, "\r\n"), "\r\n")
		if got, want := len(lines), 3+len(items); got != want {
			t.Fatalf("cols=%d: wrote %d lines, want %d", cols, got, want)
		}
		for _, line := range lines {
			if !strings.HasPrefix(line, "\r\033[K") {
				t.Errorf("cols=%d: line %q does not start on a cleared row", cols, line)
			}
			if w := visibleWidth(line); w > cols {
				t.Errorf("cols=%d: line %q spans %d cells and would wrap", cols, line, w)
			}
		}
	}
}

func TestMenuFrameClipsLongLinesWithEllipsis(t *testing.T) {
	prev := activeColorProfile
	activeColorProfile = colorprofile.ANSI256
	t.Cleanup(func() { activeColorProfile = prev })

	var buf bytes.Buffer
	f := menuFrame{w: &buf, cols: 24}
	f.row(true, "deepseek-pro", "openai · 1 models · key missing")
	got := strings.TrimSuffix(buf.String(), "\r\n")
	if !strings.HasSuffix(got, "…"+ansiReset) {
		t.Errorf("clipped row %q should end with an ellipsis inside the reverse-video span", got)
	}
	if w := visibleWidth(got); w != 24 {
		t.Errorf("clipped row spans %d cells, want 24", w)
	}

	buf.Reset()
	f.row(false, "deepseek-pro", "openai")
	if got := buf.String(); strings.Contains(got, "…") {
		t.Errorf("row that fits was clipped: %q", got)
	}

	buf.Reset()
	menuFrame{w: &buf, cols: 0}.row(true, "deepseek-pro", "openai · 1 models · key missing")
	if got := buf.String(); strings.Contains(got, "…") {
		t.Errorf("unknown width must leave the row unclipped: %q", got)
	}
}

func TestTermSizeFallsBackToVT100(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if cols, rows := termSize(int(r.Fd())); cols != 80 || rows != 24 {
		t.Errorf("termSize(pipe) = %d×%d, want 80×24", cols, rows)
	}
}
