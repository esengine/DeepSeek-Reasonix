package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// StateTracker is the OSWorld 2.0 continuous-state-management core. It exists
// because OSWorld 2.0 found that agents lose implicit state (recovered file
// paths, inferred IDs, unexplored data sources) across long sessions — the
// single largest failure mode behind the 20.6% success rate ceiling.
//
// The tracker captures three layers of state:
//   - WorkingState: the current turn's active tool calls and intermediate
//     results (token-level, ephemeral).
//   - EpisodicState: a sliding window of recent turns' key facts (turn-level,
//     medium-term).
//   - ImplicitState: facts inferred but not directly stated — hidden file
//     paths, implicit configurations, values recovered from logs or error
//     messages. This is the layer most easily lost to compaction.
//
// The interface is host-agnostic so HERMES can implement its own backend
// (file-based, SQLite, vector store) while Reasonix uses the in-memory default.
// This is the kernel-level seam for "HERMES optimizes on top of Reasonix."
type StateTracker interface {
	// BeforeToolCall records the state preceding a tool call: the call name,
	// arguments, and any caller-provided context. Returns a token the caller
	// passes to AfterToolCall to pair the pre/post snapshots.
	BeforeToolCall(ctx context.Context, call provider.ToolCall) ToolCallToken

	// AfterToolCall records the tool's result and computes the state delta
	// against the matching BeforeToolCall snapshot. The delta is what the
	// tracker feeds into ImplicitState extraction.
	AfterToolCall(ctx context.Context, token ToolCallToken, result string, err error)

	// SnapshotImplicitState returns the accumulated implicit-state entries
	// formatted for injection into a compaction summary. Called by compact.go
	// when building the "Hidden state & recovered facts" section so the
	// summarizer has concrete facts to preserve rather than having to mine
	// them from the raw transcript.
	SnapshotImplicitState() string

	// RecentEpisodes returns the sliding window of episodic entries, newest
	// first. The window size is configurable; the default keeps the last 20
	// tool-call rounds.
	RecentEpisodes() []EpisodicEntry

	// Reset clears all three layers. Called when a new session starts or the
	// agent is re-bound to a different controller/project.
	Reset()
}

// ToolCallToken pairs a BeforeToolCall snapshot with its AfterToolCall delta.
// It is opaque to callers; the default implementation uses an incrementing
// counter to index into an internal slice.
type ToolCallToken struct {
	seq int64
}

// EpisodicEntry is one turn's key facts distilled into a compact record. The
// tracker keeps a bounded window of these so the agent can reason about recent
// history without re-reading the full transcript.
type EpisodicEntry struct {
	Turn       int       // step number within the current Run
	Timestamp  time.Time // when the tool call completed
	ToolName   string    // the tool that was called
	ArgsDigest string    // short digest of the arguments (not the full args)
	ResultHint string    // first N chars of the result, for quick scanning
	Success    bool      // whether the tool returned an error
	Implicit   []string  // implicit facts extracted from this call
}

// ImplicitEntry is a recovered fact that was not directly stated in the
// transcript but inferred from tool results, error messages, or cross-source
// reasoning. These are the facts OSWorld 2.0 found agents lose after
// compaction.
type ImplicitEntry struct {
	Source    string // tool call or reasoning step that revealed this fact
	Fact      string // the recovered fact itself (path, ID, config value, etc.)
	Extracted time.Time
}

// defaultStateTracker is the Reasonix in-memory implementation. It is safe for
// concurrent use by the run loop (single writer) and Snapshot/RecentEpisodes
// readers (the compaction path, status line, etc.).
type defaultStateTracker struct {
	mu sync.RWMutex

	working     []provider.ToolCall // current turn's active calls (cleared each turn)
	episodic    []EpisodicEntry     // sliding window of recent turns
	implicit    []ImplicitEntry     // accumulated recovered facts
	pending     []preCallSnapshot   // BeforeToolCall entries awaiting AfterToolCall
	seq         int64               // monotonically increasing token counter
	maxEpisodic int                 // window size (default 20)
	sink        event.Sink          // optional event sink for diagnostics
}

type preCallSnapshot struct {
	seq       int64
	toolName  string
	args      string
	timestamp time.Time
}

// NewDefaultStateTracker creates the in-memory StateTracker used when the host
// does not provide its own implementation. maxEpisodic controls how many
// recent turns the sliding window retains; 0 means use the default (20).
func NewDefaultStateTracker(maxEpisodic int, sink event.Sink) StateTracker {
	if maxEpisodic <= 0 {
		maxEpisodic = 20
	}
	return &defaultStateTracker{
		maxEpisodic: maxEpisodic,
		sink:        sink,
	}
}

func (s *defaultStateTracker) BeforeToolCall(ctx context.Context, call provider.ToolCall) ToolCallToken {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	snap := preCallSnapshot{
		seq:       s.seq,
		toolName:  call.Name,
		args:      call.Arguments,
		timestamp: time.Now(),
	}
	s.pending = append(s.pending, snap)
	s.working = append(s.working, call)
	return ToolCallToken{seq: snap.seq}
}

func (s *defaultStateTracker) AfterToolCall(ctx context.Context, token ToolCallToken, result string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find and remove the matching pre-call snapshot.
	var snap *preCallSnapshot
	idx := -1
	for i := range s.pending {
		if s.pending[i].seq == token.seq {
			snap = &s.pending[i]
			idx = i
			break
		}
	}
	if idx < 0 {
		// No matching pre-call: the token was stale or from a different tracker.
		return
	}
	s.pending = append(s.pending[:idx], s.pending[idx+1:]...)

	// Remove this call from the working set.
	for i := range s.working {
		if s.working[i].Name == snap.toolName {
			s.working = append(s.working[:i], s.working[i+1:]...)
			break
		}
	}

	// Build the episodic entry.
	success := err == nil
	resultHint := result
	if len(resultHint) > 200 {
		resultHint = resultHint[:200] + "..."
	}
	argsDigest := digestArgs(snap.args)
	entry := EpisodicEntry{
		Turn:       len(s.episodic), // approximate turn number
		Timestamp:  snap.timestamp,
		ToolName:   snap.toolName,
		ArgsDigest: argsDigest,
		ResultHint: resultHint,
		Success:    success,
	}

	// Extract implicit facts from the result. This is the core OSWorld 2.0
	// defense: scan tool results for file paths, IDs, config values, and
	// error-recovered state that the model inferred but did not state
	// explicitly. These are the facts most easily lost to compaction.
	implicit := extractImplicitFacts(snap.toolName, result, err)
	entry.Implicit = implicit
	for _, fact := range implicit {
		s.implicit = append(s.implicit, ImplicitEntry{
			Source:    snap.toolName,
			Fact:      fact,
			Extracted: time.Now(),
		})
	}

	// Append to the episodic window, evicting the oldest if over capacity.
	s.episodic = append(s.episodic, entry)
	if len(s.episodic) > s.maxEpisodic {
		s.episodic = s.episodic[len(s.episodic)-s.maxEpisodic:]
	}

	// Emit a diagnostic so the user can see implicit state being captured.
	if s.sink != nil && len(implicit) > 0 {
		s.sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelInfo,
			Text:   fmt.Sprintf("implicit state captured from %s: %d fact(s)", snap.toolName, len(implicit)),
			Detail: fmt.Sprintf("facts: %s. These are preserved across compaction so the agent does not lose recovered context.", strings.Join(implicit, "; ")),
		})
	}
}

func (s *defaultStateTracker) SnapshotImplicitState() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.implicit) == 0 {
		return ""
	}
	var lines []string
	for _, entry := range s.implicit {
		lines = append(lines, fmt.Sprintf("- [%s] %s", entry.Source, entry.Fact))
	}
	return strings.Join(lines, "\n")
}

func (s *defaultStateTracker) RecentEpisodes() []EpisodicEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EpisodicEntry, len(s.episodic))
	copy(out, s.episodic)
	return out
}

func (s *defaultStateTracker) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.working = nil
	s.episodic = nil
	s.implicit = nil
	s.pending = nil
	s.seq = 0
}

// digestArgs produces a short, human-readable digest of tool-call arguments for
// the episodic entry. It does not store the full arguments — the goal is quick
// scanning, not replay.
func digestArgs(args string) string {
	if len(args) == 0 {
		return "(none)"
	}
	// Try to extract a few common identifying fields.
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err == nil {
		var parts []string
		for _, key := range []string{"path", "file_path", "file", "command", "cmd", "url", "id", "name"} {
			if v, ok := m[key]; ok {
				parts = append(parts, fmt.Sprintf("%s=%v", key, v))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	// Fall back to a truncated raw digest.
	s := string(args)
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return s
}

// extractImplicitFacts scans a tool result for facts that were inferred but
// not directly stated in the user's request. This is the OSWorld 2.0
// "implicit state" defense: file paths recovered from error messages, IDs
// extracted from API responses, config values discovered indirectly.
//
// The current implementation uses lightweight heuristics. A future version can
// plug in an LLM-based extractor for richer inference, but the heuristic
// baseline already captures the most common loss patterns.
func extractImplicitFacts(toolName string, result string, err error) []string {
	var facts []string

	// Error messages often reveal paths, IDs, and config values the agent
	// never stated explicitly. "file not found: /path/to/X" tells the agent
	// where the file was expected, even though the read failed.
	if err != nil {
		facts = append(facts, fmt.Sprintf("error from %s: %v", toolName, err))
	}

	// Extract file paths from the result. Paths are the most common implicit
	// state lost to compaction: the agent reads /a/b/c.go, reasons about it,
	// and after compaction cannot remember which file it was inspecting.
	paths := extractPaths(result)
	for _, p := range paths {
		facts = append(facts, fmt.Sprintf("path observed in %s result: %s", toolName, p))
	}

	// Extract IDs (numeric or alphanumeric identifiers) that may be session
	// or entity IDs the agent needs for subsequent calls.
	ids := extractIDs(result)
	for _, id := range ids {
		facts = append(facts, fmt.Sprintf("id observed in %s result: %s", toolName, id))
	}

	// Deduplicate: the same path or ID may appear in multiple results.
	seen := map[string]bool{}
	out := facts[:0]
	for _, f := range facts {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// extractPaths finds file-system-like paths in a tool result. It matches both
// Unix (/a/b/c) and Windows (C:\a\b\c) path patterns, including common
// extensions.
var (
	unixPathRe    = regexp.MustCompile(`/[a-zA-Z0-9._\-/]+\.(go|rs|py|js|ts|md|json|yaml|yml|toml|txt)`)
	windowsPathRe = regexp.MustCompile(`[A-Z]:\\[a-zA-Z0-9._\-\\]+\.(go|rs|py|js|ts|md|json|yaml|yml|toml|txt)`)
)

func extractPaths(text string) []string {
	var paths []string
	paths = append(paths, unixPathRe.FindAllString(text, -1)...)
	paths = append(paths, windowsPathRe.FindAllString(text, -1)...)
	return paths
}

// extractIDs finds identifier-like tokens in a tool result. It looks for
// patterns like "id=12345", "ID: abc123", or JSON "id":"..." fields.
var (
	idAssignRe = regexp.MustCompile(`[Ii][Dd][=: ]\s*([a-zA-Z0-9_-]{3,40})`)
	jsonIDRe   = regexp.MustCompile(`"id"\s*:\s*"([a-zA-Z0-9_-]{3,40})"`)
)

func extractIDs(text string) []string {
	var ids []string
	for _, m := range idAssignRe.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			ids = append(ids, m[1])
		}
	}
	for _, m := range jsonIDRe.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			ids = append(ids, m[1])
		}
	}
	return ids
}
