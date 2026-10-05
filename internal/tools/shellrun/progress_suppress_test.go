package shellrun

import (
	"strings"
	"testing"
)

func collectProgress(t *testing.T, suppress string, chunks ...string) string {
	t.Helper()
	var got strings.Builder
	w := newProgressWriter(func(chunk string) { got.WriteString(chunk) }, 1<<20, "")
	w.suppress = suppress
	for _, chunk := range chunks {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return got.String()
}

func TestProgressWriterDropsSuppressedLine(t *testing.T) {
	got := collectProgress(t, "__rx_report", "ok\tlogstat\n__rx_report 1 0\n")
	if got != "ok\tlogstat\n" {
		t.Fatalf("progress = %q", got)
	}
}

// The report arrives at the end of a run, so it routinely lands split across
// writes. Holding the partial tail is what keeps it off the live stream.
func TestProgressWriterDropsSuppressedLineSplitAcrossWrites(t *testing.T) {
	got := collectProgress(t, "__rx_report", "ok\tlogstat\n__rx_re", "port 1 0\n")
	if got != "ok\tlogstat\n" {
		t.Fatalf("progress = %q", got)
	}
}

func TestProgressWriterLeavesOrdinaryOutputAlone(t *testing.T) {
	const out = "building...\nok\tlogstat\t0.4s\n"
	if got := collectProgress(t, "__rx_report", out); got != out {
		t.Fatalf("progress = %q, want %q", got, out)
	}
	if got := collectProgress(t, "", out); got != out {
		t.Fatalf("unsuppressed progress = %q, want %q", got, out)
	}
}

func TestProgressWriterReportsFullWriteLength(t *testing.T) {
	w := newProgressWriter(func(string) {}, 1<<20, "")
	w.suppress = "__rx_report"
	chunk := []byte("__rx_report 0 0\n")
	n, err := w.Write(chunk)
	if err != nil || n != len(chunk) {
		t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(chunk))
	}
}
