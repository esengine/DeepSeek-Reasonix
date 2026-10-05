package report

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	got := Build([]string{"beta", "Alpha"})
	want := "   1 Alpha\n   2 beta\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReportIsFastEnough(t *testing.T) {
	rows := make([]string, 40000)
	for i := range rows {
		rows[i] = fmt.Sprintf("row-%06d", i)
	}
	start := time.Now()
	out := Build(rows)
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("Build took %v, want under 400ms", elapsed)
	}
	if n := strings.Count(out, "\n"); n != len(rows) {
		t.Fatalf("got %d lines, want %d", n, len(rows))
	}
}
