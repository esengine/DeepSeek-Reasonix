package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

// Only a screenshot the browser reports as a local file is a look; acting on a
// page, or a screenshot of a site, names no written file.
func TestOnlyALocalScreenshotRecordsAView(t *testing.T) {
	file := evidence.FileURL(filepath.Join(testenv.TempDir(t), "pelican.svg"))
	cases := []struct {
		tool, url string
		want      bool
	}{
		{"browser_read", file, true},
		{"browser_act", file, false},
		{"browser_read", "https://example.com/pelican.svg", false},
	}
	for _, c := range cases {
		rec := evidence.Receipt{ToolName: c.tool}
		decorateExecutionReceipt(&rec, "ok", &tool.ShellExecution{Kind: browserExecutionKind, State: tool.ShellStateCompleted, Subject: c.url})
		if got := len(rec.Viewed) == 1; got != c.want {
			t.Errorf("%s on %s: viewed = %v, want %v", c.tool, c.url, rec.Viewed, c.want)
		}
	}
}
