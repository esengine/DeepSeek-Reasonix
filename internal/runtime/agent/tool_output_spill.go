package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"runtime"
	"strings"

	"reasonix/internal/base/textutil"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/store"
)

// Oversize tool output goes to a file; the context keeps a pointer. A head+tail
// sketch is a guess about which part mattered, made by the wrong party — only
// the model knows, and read_file plus grep let it decide.

// A spill is a fixed price: the instructions, plus at most the same again in
// preview. Counting the preview in lines would instead make a wide result cost
// more to leave context than a narrow one — the opposite of the point.

// spillToolOutput writes body beside the session and returns the pointer that
// replaces it, plus where it landed. ok is false when there is nowhere the model
// could read the file back from, or when spilling would not pay for itself.
// There is deliberately no size threshold: context pays the pointer, the pointer
// is a pure function of the body, so the decision is derived rather than configured.
func (a *Agent) spillToolOutput(body, toolName, toolCallID string) (string, string, bool) {
	dir := a.spillDir()
	if dir == "" {
		return "", "", false
	}
	path := filepath.Join(dir, spillFileName(toolName, toolCallID))
	pointer := spillPointer(path, body, toolName)
	if !spillPays(len(body), len(pointer)) {
		return "", "", false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", false
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", "", false
	}
	return pointer, path, true
}

// spillPays reports whether moving a result out of context earns its keep. What
// is spent is the pointer plus the turn spent reading it back, and that turn was
// what comparing bytes alone kept leaving out: a body a single fetch recovers
// whole buys both to arrive exactly where leaving it would have. Past that limit
// the model reads a window instead, which is the case a pointer exists for.
func spillPays(body, pointer int) bool {
	if body <= MaxToolOutputBytes {
		return false
	}
	return body-pointer >= pointer
}

// spillDir is where this agent's oversize results go: beside its transcript
// when it has one, the archive it inherited otherwise, and failing both a
// scratch directory. The last is what keeps the lossless path open — with
// nowhere to write, the only remaining bound is truncation, which discards
// almost all of a large result rather than parking it.
func (a *Agent) spillDir() string {
	if dir := store.SessionOutputsDir(a.SessionPath()); dir != "" {
		return dir
	}
	if dir := a.archiveOutputsDir(); dir != "" {
		return dir
	}
	return sessionstore.ScratchOutputsDir()
}

// toolIsPaged reports whether this tool addresses its own continuation, read off
// its contract rather than its name.
func (a *Agent) toolIsPaged(name string) bool {
	p, ok := toolCapability[tool.Paged](a.svc.tools, name)
	return ok && p.Paged()
}

// toolCapability type-asserts a registered tool to one of its optional capability
// interfaces. An unregistered tool and one lacking the capability are the same
// answer to a caller, so both report false.
func toolCapability[T any](reg *tool.Registry, name string) (T, bool) {
	var zero T
	if reg == nil {
		return zero, false
	}
	t, ok := reg.Get(name)
	if !ok {
		return zero, false
	}
	c, ok := t.(T)
	return c, ok
}

// readsSpilledOutput reports whether this call is the model fetching a spilled
// result back. Such a fetch must never spill: it would return a pointer to a
// further file, and read_file's line numbers grow the body each round, so the
// loop has no exit. The tool names the path it reads because only it knows its
// own arguments; a shell names none, so its output spills as usual.
func (a *Agent) readsSpilledOutput(toolName, toolArgs string) bool {
	rt, ok := toolCapability[tool.ReadTargeter](a.svc.tools, toolName)
	if !ok {
		return false
	}
	target := rt.ReadTarget(json.RawMessage(toolArgs))
	if target == "" {
		return false
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	// A spill lands directly in the directory, so the file's own parent is the
	// only candidate; grep aimed at the directory itself matches as-is.
	for _, dir := range []string{a.spillDir(), a.archiveOutputsDir()} {
		if dir == "" {
			continue
		}
		if sameDir(filepath.Dir(abs), dir) || sameDir(abs, dir) {
			return true
		}
	}
	return false
}

// sameDir compares paths the way the host filesystem resolves them. Erring
// toward equal is the safe side: a false match only forgoes one spill, while a
// missed one restores the read-back loop.
func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// archiveOutputsDir is where an agent with no transcript of its own spills.
// Sub-agents are the case that matters: they carry no session path, so without
// this every oversize result they saw was truncated while the lossless path sat
// one field away. The archive already holds content moved out of context, and
// they inherit its location from the parent.
func (a *Agent) archiveOutputsDir() string {
	root := strings.TrimSpace(a.archiveDir)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "outputs")
}

// spillFileName keys the file by tool call, so a directory listing reads as the
// turn's history and a re-read finds the same file.
func spillFileName(toolName, toolCallID string) string {
	base := strings.TrimSpace(toolCallID)
	if base == "" {
		base = strings.TrimSpace(toolName)
	}
	if base == "" {
		base = "output"
	}
	safe := make([]rune, 0, len(base))
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			safe = append(safe, r)
		default:
			safe = append(safe, '-')
		}
	}
	trimmed := strings.Trim(string(safe), "-")
	if trimmed == "" {
		trimmed = "output"
	}
	if len(trimmed) > 96 {
		trimmed = trimmed[:96]
	}
	return trimmed + ".txt"
}

// spillPointer is what the model sees: the size, the path, how to get the rest,
// then as much of the start as the instructions themselves cost.
func spillPointer(path, body, toolName string) string {
	instr := spillInstructions(path, body, toolName)
	head := headWithin(body, len(instr))
	if head == "" {
		return instr
	}
	return instr + "\nOpening lines:\n" + head + "\n"
}

// spillInstructions is the irreducible part: where the output went and how to
// read it back. Its size is what a spill really costs, so it also bounds the
// preview — the model is told the shape, not shown a guess at the substance.
func spillInstructions(path, body, toolName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s output kept out of context: %d lines, %s]\n", displayToolName(toolName), strings.Count(body, "\n")+1, textutil.HumanBytes(int64(len(body))))
	fmt.Fprintf(&b, "Full output: %s\n", path)
	b.WriteString("Read a window of it with read_file (offset/limit), or search it with grep. It is not in this conversation.\n")
	return b.String()
}

// headWithin returns the whole leading lines of body that fit in max bytes, so a
// preview never cuts mid-line and never exceeds its budget.
func headWithin(body string, max int) string {
	end := 0
	for end < len(body) {
		nl := strings.IndexByte(body[end:], '\n')
		if nl < 0 {
			if len(body) <= max {
				end = len(body)
			}
			break
		}
		if end+nl+1 > max {
			break
		}
		end += nl + 1
	}
	return strings.TrimRight(body[:end], "\n")
}

func displayToolName(name string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return "tool"
}

// boundToolOutput is the single gate every tool result passes before it becomes
// context. The bound records which of the three outcomes happened, so a frontend
// can tell a result that moved to disk from one that lost its middle; the last
// return is the truncation notice, empty unless something was lost. failed comes
// from the caller because reading it back out of the body would guess at wording.
func (a *Agent) boundToolOutput(body, toolName, toolCallID, toolArgs string, failed bool) (string, event.OutputBound, string) {
	bound := event.OutputBound{Lines: strings.Count(body, "\n") + 1, Bytes: len(body)}
	paged := a.toolIsPaged(toolName) || a.readsSpilledOutput(toolName, toolArgs)
	if !paged {
		if pointer, path, ok := a.spillToolOutput(body, toolName, toolCallID); ok {
			bound.Kind, bound.Path = event.BoundSpilled, path
			return pointer, bound, ""
		}
	}
	if len(body) <= MaxToolOutputBytes {
		bound.KeptBytes = len(body)
		return body, bound, ""
	}
	// A paged read addresses its own continuation, so cutting it back to a leading
	// window discards nothing: the model reads on from where this stops. Snipping
	// its middle would, and would also undo the paging the tool already did.
	if paged {
		out := windowToolOutput(body, MaxToolOutputBytes)
		bound.Kind, bound.KeptBytes = event.BoundWindowed, len(out)
		return out, bound, ""
	}
	strategy := a.window().snipStrategyFor(toolName)
	if failed {
		strategy = strategy.forFailure(MaxToolOutputBytes)
	}
	out, notice := truncateToolOutputFor(body, toolName, toolCallID, MaxToolOutputBytes, strategy)
	bound.Kind, bound.KeptBytes = event.BoundTruncated, len(out)
	return out, bound, notice
}

const windowMarker = "\n…[window ends here to fit one result — nothing was discarded; read on from where this stops]…\n"

// windowToolOutput cuts body back to whole leading lines that fit in cap. What
// is left out stays reachable by continuing the page, so this is a window, not
// a loss — which is why it carries no truncation notice.
func windowToolOutput(body string, cap int) string {
	keep := cap - len(windowMarker) - 8
	if keep < 1 || len(body) <= keep {
		return body
	}
	cut := strings.LastIndexByte(body[:keep], '\n')
	if cut <= 0 {
		cut = len(snapToRuneBoundary(body, 0, keep))
	}
	return body[:cut] + windowMarker
}
