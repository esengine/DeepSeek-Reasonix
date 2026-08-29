package outputstyle

import (
	"testing"
)

func TestDescribeList(t *testing.T) {
	style := func(name, desc string, builtin bool) OutputStyle {
		return OutputStyle{Name: name, Description: desc, Builtin: builtin}
	}

	tests := []struct {
		name   string
		styles []OutputStyle
		active string
		want   string
	}{
		{
			name: "empty list",
			want: "",
		},
		{
			name:   "single builtin",
			styles: []OutputStyle{style("explanatory", "Explain choices", true)},
			want:   "  explanatory (builtin) — Explain choices",
		},
		{
			name: "multiple items no trailing newline",
			styles: []OutputStyle{
				style("concise", "Terse replies", true),
				style("pirate", "Arrr", false),
			},
			want: "  concise (builtin) — Terse replies\n  pirate (custom) — Arrr",
		},
		{
			name:   "active item marked with asterisk",
			styles: []OutputStyle{style("learning", "Learn", true), style("pirate", "Arrr", false)},
			active: "learning",
			want:   "* learning (builtin) — Learn\n  pirate (custom) — Arrr",
		},
		{
			name:   "active match is case-insensitive",
			styles: []OutputStyle{style("concise", "Terse", true)},
			active: "CONCISE",
			want:   "* concise (builtin) — Terse",
		},
		{
			name:   "empty description",
			styles: []OutputStyle{style("ghost", "", false)},
			active: "ghost",
			want:   "* ghost (custom) — ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DescribeList(tt.styles, tt.active); got != tt.want {
				t.Errorf("DescribeList() = %q, want %q", got, tt.want)
			}
		})
	}
}
