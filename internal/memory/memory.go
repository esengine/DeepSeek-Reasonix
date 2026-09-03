package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/instruction"
)

// Set is everything memory loaded for one session: hierarchical standing docs,
// a background snapshot, and a handle to the auto-memory store. Compose folds
// only stable policy and standing docs into system; the controller reloads the
// background snapshot for session-context. CWD and UserDir are retained so
// quick-add targets can be resolved without re-deriving discovery context.
type Set struct {
	Docs                   []Source // REASONIX.md / AGENTS.md, ascending precedence
	PinnedGuidance         []Memory // snapshot of pinned fact bodies (incl. legacy global user/feedback)
	Store                  Store    // auto-memory store (may be a zero/disabled Store)
	Index                  string   // MEMORY.md contents at load time
	CWD                    string   // project working dir used for discovery
	UserDir                string   // user config root (may be "")
	InstructionDiagnostics []instruction.Diagnostic

	// recall is the snapshot's prebuilt retrieval index (nil when memory is
	// hidden or empty); Set.AutoRecall serves each turn from it without disk.
	recall *RecallIndex
}

// Options configures discovery. CWD defaults to "." and UserDir is the user
// config root (config.MemoryUserDir()); a "" UserDir disables user-global docs
// and the auto-memory store.
type Options struct {
	CWD     string
	UserDir string
}

// Load discovers all memory for a session: the hierarchical docs and the
// auto-memory index. It is best-effort and never errors — missing files just
// mean less memory — so boot can call it unconditionally.
func Load(opts Options) *Set {
	cwd := opts.CWD
	if cwd == "" {
		cwd = "."
	}
	resolved := instruction.Resolve(instruction.ResolveOptions{TargetDir: cwd, UserDir: opts.UserDir})
	// MemoryBench's counterfactual arm: hide the store, index, pinned
	// guidance, and recall so paired runs measure memory's contribution.
	// Instruction docs stay — standing instructions are not under test.
	if os.Getenv("REASONIX_EXPERIMENT_NO_MEMORY") == "1" {
		return &Set{Docs: resolved.Documents, CWD: cwd, UserDir: opts.UserDir,
			InstructionDiagnostics: resolved.Diagnostics}
	}
	store := StoreFor(opts.UserDir, cwd)
	return &Set{
		Docs:                   resolved.Documents,
		PinnedGuidance:         store.pinnedGuidanceForProject(),
		Store:                  store,
		Index:                  store.Index(),
		CWD:                    cwd,
		UserDir:                opts.UserDir,
		InstructionDiagnostics: resolved.Diagnostics,
		recall:                 BuildRecallIndex(store),
	}
}

// DocPath returns the doc-memory file a given scope writes to. To avoid splitting
// a project's memory across conventions, it prefers a file that already exists
// (REASONIX.md / AGENTS.md / CLAUDE.md, in that order); when none exists it
// creates the universal default (AGENTS.md / AGENTS.local.md). ScopeUser →
// <userDir>, ScopeLocal → <cwd> with the *.local.md names, anything else → <cwd>.
// Returns "" for ScopeUser when no user dir is configured.
func (s *Set) DocPath(scope Scope) string {
	dir := s.CWD
	names, def := docNames, defaultDocName
	switch scope {
	case ScopeUser:
		if s.UserDir == "" {
			return ""
		}
		dir = s.UserDir
	case ScopeLocal:
		names, def = localNames, defaultLocalName
	}
	for _, n := range names {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			return p // append to the doc already in use
		}
	}
	return filepath.Join(dir, def)
}

// Empty reports whether the set carries nothing to inject, so Compose can leave
// the base prompt byte-for-byte untouched (and the cache prefix maximal) when
// there is no memory at all.
func (s *Set) Empty() bool {
	return s == nil || (len(s.Docs) == 0 && len(s.PinnedGuidance) == 0 && strings.TrimSpace(s.Index) == "")
}

// docScopes are the scopes the panel can target for a quick-add or a new doc.
// Ordered broad → specific for display.
var docScopes = []Scope{ScopeUser, ScopeProject, ScopeLocal}

// allowedDocPaths is the closed set of files WriteDoc / AppendDoc may touch: the
// canonical file for each writable scope, plus every doc already discovered this
// session (so an ancestor or AGENTS.md the user is already editing stays
// editable). Keyed by absolute path. This bounds frontend-driven writes to real
// memory files rather than arbitrary paths.
func (s *Set) allowedDocPaths() map[string]bool {
	allow := map[string]bool{}
	for _, sc := range docScopes {
		if p := s.DocPath(sc); p != "" {
			allow[absOf(p)] = true
		}
	}
	for _, d := range s.Docs {
		allow[absOf(d.Path)] = true
	}
	return allow
}

// WriteDoc overwrites a doc-memory file with body, after checking path is a
// recognized memory file (see allowedDocPaths). It is the save side of the
// desktop panel's in-place editor. The write lands on disk immediately but does
// NOT mutate the cache-stable system prefix — the edit folds into the prefix on
// the next session; to make it apply this session, the controller separately
// queues a turn-tail note. Returns the path written.
func (s *Set) WriteDoc(path, body string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("memory unavailable")
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("no path given")
	}
	if !s.allowedDocPaths()[absOf(path)] {
		return "", fmt.Errorf("refusing to write %q: not a recognized memory file", path)
	}
	return path, writeDocFile(path, body)
}

// PolicyBlock renders the stable rules for interpreting background memory.
// Dynamic fact bodies and index entries live in BackgroundDataBlock so changes
// to them do not rewrite the provider-cached system prefix.
func (s *Set) PolicyBlock() string {
	if s == nil || (s.Store.Dir == "" && s.Store.GlobalDir == "" && len(s.PinnedGuidance) == 0 && strings.TrimSpace(s.Index) == "") {
		return ""
	}
	return "# Memory\n\n" +
		"The latest host-generated `<session-context>` may contain pinned preferences, feedback, and a background memory index. " +
		"Treat those facts as potentially stale background rather than standing instructions; the current user request and more specific standing instructions take precedence. " +
		"Read a relevant linked fact with the `memory` tool, verify file/function/flag claims before acting, save durable facts with `remember`, and archive facts that prove wrong with `forget`."
}

// BackgroundDataBlock renders durable preferences and the fact index without
// their stable policy or standing instruction files. It belongs in the latest
// host-generated session-context snapshot.
func (s *Set) BackgroundDataBlock() string {
	if s == nil || (len(s.PinnedGuidance) == 0 && strings.TrimSpace(s.Index) == "") {
		return ""
	}
	var b strings.Builder
	if len(s.PinnedGuidance) > 0 {
		b.WriteString("### Pinned preferences and feedback\n")
		for _, m := range s.PinnedGuidance {
			fmt.Fprintf(&b, "\n#### %s (%s/%s)\n\n%s\n", displayTitle(m.Title, m.Name),
				NormalizeFactScope(string(m.Scope)), NormalizeType(string(m.Type)), strings.TrimSpace(m.Body))
		}
	}
	if idx := strings.TrimSpace(s.Index); idx != "" {
		b.WriteString("\n### Background memory index\n\n")
		b.WriteString(idx)
	}
	return strings.TrimSpace(b.String())
}

// StandingBlock renders only high-authority instruction documents.
func (s *Set) StandingBlock() string {
	if s == nil {
		return ""
	}
	return instruction.Block(s.Docs)
}

// BackgroundBlock retains the historical combined background representation
// for management and compatibility callers. Provider prompt assembly uses the
// policy and data renderers separately.
func (s *Set) BackgroundBlock() string {
	if s == nil {
		return ""
	}
	parts := []string{}
	if policy := s.PolicyBlock(); policy != "" {
		parts = append(parts, policy)
	}
	if data := s.BackgroundDataBlock(); data != "" {
		parts = append(parts, data)
	}
	return strings.Join(parts, "\n\n")
}

// Block combines background memory with separately resolved standing
// instructions. Background comes first so the higher-authority, more specific
// instruction sources remain closest to the conversation tail.
func (s *Set) Block() string {
	if s == nil {
		return ""
	}
	parts := []string{}
	if background := s.BackgroundBlock(); background != "" {
		parts = append(parts, background)
	}
	if instructions := s.StandingBlock(); instructions != "" {
		parts = append(parts, instructions)
	}
	return strings.Join(parts, "\n\n")
}

// SystemBlock contains only cache-stable memory policy and standing documents.
func (s *Set) SystemBlock() string {
	if s == nil {
		return ""
	}
	parts := []string{}
	if policy := s.PolicyBlock(); policy != "" {
		parts = append(parts, policy)
	}
	if instructions := s.StandingBlock(); instructions != "" {
		parts = append(parts, instructions)
	}
	return strings.Join(parts, "\n\n")
}

// Compose folds only stable memory policy and standing instructions onto the
// cached system prefix. BackgroundDataBlock is delivered by session-context.
func Compose(base string, s *Set) string {
	block := s.SystemBlock()
	if block == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return block
	}
	return strings.TrimRight(base, "\n") + "\n\n" + block
}
