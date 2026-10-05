package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

const spillMarker = "kept out of context"

// Moving a result out of context must never make the context bigger. A size
// threshold cannot promise this: set one below what the pointer costs and every
// body in between is replaced by something longer than itself.
func TestSpillNeverGrowsTheContext(t *testing.T) {
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{ArchiveDir: testenv.TempDir(t)}, event.Discard)
	line := strings.Repeat("x", 78) + "\n"
	for _, lines := range []int{1, 4, 13, 20, 26, 40, 64, 200, 800, 2000} {
		body := strings.Repeat(line, lines)
		out, _, notice := a.boundToolOutput(body, "bash", fmt.Sprintf("call-%d", lines), "", false)
		if len(out) > len(body) {
			t.Errorf("%d lines (%d bytes) came back as %d bytes", lines, len(body), len(out))
		}
		if notice == "" && !strings.Contains(out, spillMarker) && out != body {
			t.Errorf("%d lines: neither spilled nor truncated nor returned as-is", lines)
		}
	}
}

// A result the model could fetch back in a single read has nothing to gain from
// leaving: the pointer and the extra turn buy a trip to where it already was.
// Everyday bash and read_file output lives in this range.
func TestResultsThatFitOneReadBackStayInContext(t *testing.T) {
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{ArchiveDir: testenv.TempDir(t)}, event.Discard)
	for _, size := range []int{1 << 10, 4 << 10, 16 << 10, MaxToolOutputBytes} {
		body := strings.Repeat("x", size)
		out, _, notice := a.boundToolOutput(body, "bash", fmt.Sprintf("fit-%d", size), "", false)
		if out != body || notice != "" {
			t.Errorf("%d bytes came back changed (notice=%q); one fetch recovers it whole, so it should stay", size, notice)
		}
	}
}

// The pointer's whole promise is that the model can read the result back. A
// fetch that spills returns another pointer to another file, and read_file
// numbers what it returns, so every round is bigger than the last: the content
// never arrives. This is the loop that issued 1936 reads in one session.
func TestReadingASpillBackDoesNotSpillAgain(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(pagedReader{})
	a := New(nil, reg, sessionstore.NewSession("sys"), Options{ArchiveDir: testenv.TempDir(t)}, event.Discard)
	body := strings.Repeat(strings.Repeat("payload ", 12)+"\n", 500)
	out, _, _ := a.boundToolOutput(body, "bash", "call_00_ORIGIN", "", false)
	if !strings.Contains(out, spillMarker) {
		t.Fatalf("a %d-byte body should have spilled", len(body))
	}
	path := spillPathIn(t, out)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the model cannot read its own spill back: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	args := fmt.Sprintf(`{"path":%q}`, path)

	// A window, the way read_file is meant to be used: it must arrive verbatim.
	window := numberLines(lines[:200])
	got, _, notice := a.boundToolOutput(window, "read_file", "call_01_WINDOW", args, false)
	if got != window || notice != "" {
		t.Errorf("a %d-byte window came back changed (notice=%q); the fetch must deliver", len(window), notice)
	}

	// The whole file, repeatedly: bounded however it likes, but never by
	// spilling — that is the step which has no exit.
	for round := 1; round <= 5; round++ {
		whole := numberLines(lines)
		got, _, _ := a.boundToolOutput(whole, "read_file", fmt.Sprintf("call_%02d_WHOLE", round), args, false)
		if strings.Contains(got, spillMarker) {
			t.Fatalf("round %d: the fetch was spilled again, so the model gets a pointer where it asked for content", round)
		}
	}
}

// Leaving context is a fixed price, so the bar cannot move with the shape of the
// output. A preview counted in lines would break this: wide lines would make a
// result cost more to move out of context, which is the opposite of the point.
// Bars still round to whole lines, so they are close rather than identical.
func TestSpillBarDoesNotFollowLineLength(t *testing.T) {
	lo, hi := 0, 0
	for _, lineLen := range []int{20, 40, 80, 120, 200} {
		bar := spillBarFor(t, lineLen)
		if bar == 0 {
			t.Fatalf("lineLen=%d: spilling never engaged", lineLen)
		}
		if lo == 0 || bar < lo {
			lo = bar
		}
		if bar > hi {
			hi = bar
		}
		t.Logf("lineLen=%3d bar=%5d", lineLen, bar)
	}
	if hi > 2*lo {
		t.Errorf("bars span %d..%d; a fixed-price pointer must not let line length double the bar", lo, hi)
	}
	// The bar belongs at the point where one fetch stops recovering the whole
	// result. Below it, spilling costs a turn and returns nothing for it.
	if lo <= MaxToolOutputBytes {
		t.Errorf("lowest bar = %d, at or under the %d a single fetch carries", lo, MaxToolOutputBytes)
	}
	if hi > MaxToolOutputBytes+1024 {
		t.Errorf("highest bar = %d; past the cap it should engage within one line's rounding", hi)
	}
}

// At the bar, what the spill saves covers what its pointer costs. Below the bar
// the result stays whole rather than being spilled for a rounding error, since a
// pointer the model reads back costs a further turn.
func TestSpillEarnsAtLeastItsOwnCost(t *testing.T) {
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{ArchiveDir: testenv.TempDir(t)}, event.Discard)
	line := strings.Repeat("y", 96) + "\n"
	for lines := 1; lines <= 600; lines += 3 {
		body := strings.Repeat(line, lines)
		out, _, _ := a.boundToolOutput(body, "bash", fmt.Sprintf("earn-%d", lines), "", false)
		if !strings.Contains(out, spillMarker) {
			continue
		}
		if saved, cost := len(body)-len(out), len(out); saved < cost {
			t.Fatalf("spilled %d bytes to save %d while the pointer costs %d", len(body), saved, cost)
		}
		return
	}
	t.Fatal("spilling never engaged within 600 lines")
}

// pagedReader stands in for read_file. This package's tests cannot import
// tool/builtin — that package imports back into agent — so the Paged contract is
// exercised through a stub registered under the same name.
type pagedReader struct{}

func (pagedReader) Name() string            { return "read_file" }
func (pagedReader) Description() string     { return "stub" }
func (pagedReader) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (pagedReader) ReadOnly() bool          { return true }
func (pagedReader) Paged() bool             { return true }
func (pagedReader) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (pagedReader) ReadTarget(args json.RawMessage) string { return stubReadTarget(args) }

// targetedSearch stands in for grep: it names the path it reads but has no
// continuation of its own, so the ReadTargeter contract is the only thing that
// can spare it — which is what makes it the honest test of that contract.
type targetedSearch struct{}

func (targetedSearch) Name() string            { return "grep" }
func (targetedSearch) Description() string     { return "stub" }
func (targetedSearch) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (targetedSearch) ReadOnly() bool          { return true }
func (targetedSearch) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (targetedSearch) ReadTarget(args json.RawMessage) string { return stubReadTarget(args) }

// stubReadTarget stands in for the real resolvers: the contract is that a tool
// reports the path it reads, not how it derived it.
func stubReadTarget(args json.RawMessage) string {
	var p struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &p) != nil {
		return ""
	}
	return p.Path
}

// Sparing a fetch is decided by where it points, not by which tool made it. A
// tool that cannot page is still spared inside the spill directory, and the same
// tool reading anywhere else still spills — so no name is load-bearing.
func TestFetchingASpillIsSparedByContractNotByName(t *testing.T) {
	root := testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(targetedSearch{})
	a := New(nil, reg, sessionstore.NewSession("sys"), Options{ArchiveDir: root}, event.Discard)
	body := strings.Repeat("a matching line of spilled content\n", 2000)

	inSpill := fmt.Sprintf(`{"path":%q}`, filepath.Join(a.spillDir(), "call-1.txt"))
	out, bound, notice := a.boundToolOutput(body, "grep", "call-fetch", inSpill, false)
	if strings.Contains(out, spillMarker) {
		t.Error("a fetch aimed at the spill directory must deliver content, not a further pointer")
	}
	if bound.Kind != event.BoundWindowed || notice != "" {
		t.Errorf("kind = %d notice = %q; a spared fetch is windowed and loses nothing", bound.Kind, notice)
	}

	elsewhere := fmt.Sprintf(`{"path":%q}`, filepath.Join(root, "src", "main.go"))
	out, _, _ = a.boundToolOutput(body, "grep", "call-ordinary", elsewhere, false)
	if !strings.Contains(out, spillMarker) {
		t.Error("the same tool reading outside the spill directory still spills")
	}
}

// A tool that claims no read target — a shell, whose path is knowable only by
// parsing the command — keeps spilling. The gate must not guess one for it.
func TestToolWithoutTheContractIsNeverSpared(t *testing.T) {
	root := testenv.TempDir(t)
	reg := tool.NewRegistry()
	a := New(nil, reg, sessionstore.NewSession("sys"), Options{ArchiveDir: root}, event.Discard)
	body := strings.Repeat("a wide line of ordinary command output\n", 2000)
	args := fmt.Sprintf(`{"command":"cat %s"}`, filepath.Join(a.spillDir(), "call-1.txt"))
	if a.readsSpilledOutput("bash", args) {
		t.Fatal("a shell cannot name what it reads without parsing it, so it must not be spared")
	}
	out, bound, _ := a.boundToolOutput(body, "bash", "call-shell", args, false)
	if !strings.Contains(out, spillMarker) || bound.Kind != event.BoundSpilled {
		t.Errorf("kind = %d; output that names no target still spills", bound.Kind)
	}
}

// A tool that addresses its own continuation is windowed, not moved: the model
// gets real bytes now and reads on from where they stop. Spilling would hand it
// a pointer instead of content, and point it at a numbered duplicate of a file
// already on disk.
func TestPagedReadsWindowRatherThanSpill(t *testing.T) {
	root := testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(pagedReader{})
	a := New(nil, reg, sessionstore.NewSession("sys"), Options{ArchiveDir: root}, event.Discard)
	body := strings.Repeat("a wide line of ordinary file content\n", 2000)
	args := fmt.Sprintf(`{"path":%q}`, filepath.Join(root, "somewhere", "else.go"))
	out, bound, notice := a.boundToolOutput(body, "read_file", "call-ordinary", args, false)
	if strings.Contains(out, spillMarker) {
		t.Error("a paged read should window rather than spill: it can continue from where it stops")
	}
	if bound.Kind != event.BoundWindowed {
		t.Errorf("kind = %d, want BoundWindowed", bound.Kind)
	}
	if notice != "" || bound.Lossy() {
		t.Errorf("a window discards nothing and must not report a loss (notice=%q)", notice)
	}
	if !strings.HasPrefix(body, strings.TrimSuffix(out, windowMarker)) {
		t.Error("the window must be a verbatim leading run of the body")
	}
}

// A tool with no continuation of its own still spills: its output happened once,
// so parking it somewhere readable is the only way to keep it whole.
func TestUnpagedResultsStillSpill(t *testing.T) {
	root := testenv.TempDir(t)
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{ArchiveDir: root}, event.Discard)
	body := strings.Repeat("a wide line of ordinary command output\n", 2000)
	out, bound, _ := a.boundToolOutput(body, "bash", "call-unpaged", "", false)
	if !strings.Contains(out, spillMarker) {
		t.Fatalf("a %d-byte result from a tool that cannot re-read should spill", len(body))
	}
	if bound.Kind != event.BoundSpilled || bound.Path == "" {
		t.Errorf("kind = %d, path = %q; a spill must say where it went", bound.Kind, bound.Path)
	}
}

func spillBarFor(t *testing.T, lineLen int) int {
	t.Helper()
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{ArchiveDir: testenv.TempDir(t)}, event.Discard)
	line := strings.Repeat("x", lineLen) + "\n"
	for lines := 1; lines <= 4000; lines++ {
		body := strings.Repeat(line, lines)
		out, _, _ := a.boundToolOutput(body, "bash", fmt.Sprintf("bar-%d-%d", lineLen, lines), "", false)
		if strings.Contains(out, spillMarker) {
			return len(body)
		}
	}
	return 0
}

func spillPathIn(t *testing.T, pointer string) string {
	t.Helper()
	for l := range strings.SplitSeq(pointer, "\n") {
		if rest, ok := strings.CutPrefix(l, "Full output: "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("no path in pointer:\n%s", pointer)
	return ""
}

// numberLines mimics read_file's output shape, whose per-line prefixes are why
// an unchecked read-back grows instead of converging.
func numberLines(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		fmt.Fprintf(&b, "%4d→%s\n", i+1, l)
	}
	return b.String()
}
