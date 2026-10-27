package hook

import (
	"slices"
	"testing"
)

func TestCommandWordsKeepAnUnquotedWindowsPathAsWritten(t *testing.T) {
	for _, c := range []struct{ command, want string }{
		{`scripts\start.cmd --flag`, `scripts\start.cmd`},
		{`.\tools\run.bat`, `.\tools\run.bat`},
		{`"scripts\start.cmd" --flag`, `scripts\start.cmd`},
		{`./scripts/start.sh`, `./scripts/start.sh`},
	} {
		if got := commandWords(c.command); !slices.Contains(got, c.want) {
			t.Errorf("commandWords(%q) = %q, want it to hold %q", c.command, got, c.want)
		}
	}
}
