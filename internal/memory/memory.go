package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"reasonix/internal/instruction"
)

// Set is everything memory loaded for one session: the hierarchical docs and a
// handle to the auto-memory store. It is
// assembled once at boot and folded into the system prompt by Compose. CWD and
// UserDir are retained so the controller can resolve quick-add targets without
// re-deriving discovery context.
type Set struct {
	Docs                   []Source // REASONIX.md / AGENTS.md, ascending precedence
	PinnedGuidance         []Memory // stable snapshot of pinned fact bodies (incl. legacy global user/feedback)
	Store                  Store    // auto-memory store (may be a zero/disabled Store)
	CWD                    string   // project working dir used for discovery
	UserDir                string   // user config root (may be "")
	InstructionDiagnostics []instruction.Diagnostic

	// recall is the snapshot's prebuilt retrieval index (nil when memory is
	// hidden or empty); Set.AutoRecall serves each turn from it without disk.
	recall *RecallIndex
	// opts is what this set was loaded with, so a mid-session reload keeps the
	// user's budgets instead of silently reverting them to the defaults.
	opts Options
}

// Options configures discovery. CWD defaults to "." and UserDir is the user
// config root (config.MemoryUserDir()); a "" UserDir disables user-global docs
// and the auto-memory store.
type Options struct {
	CWD     string
	UserDir string
	// Budgets are the user's; zero means unbounded for the pinned ceiling and
	// the built-in default for the two recall axes.
	PinnedBudgetChars int
	RecallLimit       int
	RecallMaxChars    int
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
			InstructionDiagnostics: resolved.Diagnostics, opts: opts}
	}
	store := StoreFor(opts.UserDir, cwd)
	store.PinnedBudgetChars = opts.PinnedBudgetChars
	return &Set{
		Docs:                   resolved.Documents,
		PinnedGuidance:         store.pinnedGuidanceForProject(),
		Store:                  store,
		CWD:                    cwd,
		UserDir:                opts.UserDir,
		InstructionDiagnostics: resolved.Diagnostics,
		recall:                 BuildRecallIndex(store),
		opts:                   opts,
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
	return s == nil || (len(s.Docs) == 0 && len(s.PinnedGuidance) == 0 && s.Store.Dir == "")
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

// BackgroundBlock renders the memory protocol and any pinned preferences. The
// saved-fact index is deliberately absent: it changes on every remember, and
// everything after it in the prefix — the project instructions, the skills
// index — would be re-sent with it. The tools reach it instead.
func (s *Set) BackgroundBlock() string {
	if s == nil {
		return ""
	}
	if s.Store.Dir == "" && len(s.PinnedGuidance) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Memory\n\n")
	if s.Store.Dir != "" {
		b.WriteString(memoryProtocol)
	}
	if len(s.PinnedGuidance) > 0 {
		if s.Store.Dir != "" {
			b.WriteString("\n\n")
		}
		b.WriteString("## Pinned preferences and feedback\n\n")
		b.WriteString("Facts the user pinned to be always available. Apply them when relevant. " +
			"The current user request and more specific standing instructions take precedence, and factual details may be stale.\n")
		for _, m := range s.PinnedGuidance {
			fmt.Fprintf(&b, "\n### %s (%s/%s)\n\n%s\n", displayTitle(m.Title, m.Name),
				NormalizeFactScope(string(m.Scope)), NormalizeType(string(m.Type)), strings.TrimSpace(m.Body))
		}
	}
	return strings.TrimSpace(b.String())
}

// memoryProtocol is how the model reaches saved facts. It names the tools and
// carries no facts of its own, so it is byte-identical for every project and
// session on a machine — which is the whole reason the index it replaced went.
const memoryProtocol = "## Background memory\n\n" +
	"Durable facts you saved in earlier sessions are stored for this project. They are not listed here: " +
	"relevant ones are retrieved onto the turn automatically, and the `memory` tool reaches the rest " +
	"(`search` ranks them, `list` returns the whole index, `read` returns one body). " +
	"They reflect what was true when written and may now be stale — treat them as background, not standing " +
	"instructions, and before acting on one that names a file, function, or flag, verify it still exists. " +
	"Save new durable facts with the `remember` tool; archive ones that turn out wrong with `forget`."

// InstructionsBlockFor renders the standing instructions as they are on disk
// now. It rediscovers rather than reading a snapshot: the block rides the turn,
// and an edit made by an editor, a script or a branch switch is still an edit.
func InstructionsBlockFor(opts Options) string {
	cwd := opts.CWD
	if cwd == "" {
		cwd = "."
	}
	return instruction.Block(instruction.Resolve(instruction.ResolveOptions{TargetDir: cwd, UserDir: opts.UserDir}).Documents)
}

// InstructionsBlock renders the standing instructions resolved for this
// workspace. It is per-project by definition, so it rides the turn: the
// controller owes it to a session once and again whenever the set changes.
func (s *Set) InstructionsBlock() string {
	if s == nil {
		return ""
	}
	return instruction.Block(s.Docs)
}

// StaticContext is background memory and instructions in one string, for a
// session that has no turn projection to ride — the planner builds its prompt
// once and never composes a user turn. Base sessions take the two separately.
func (s *Set) StaticContext() string {
	if s == nil {
		return ""
	}
	parts := []string{}
	if background := s.BackgroundBlock(); background != "" {
		parts = append(parts, background)
	}
	if instructions := s.InstructionsBlock(); instructions != "" {
		parts = append(parts, instructions)
	}
	return strings.Join(parts, "\n\n")
}

// Compose folds background memory onto the base system prompt and returns the
// durable cached-prefix string. Instructions are deliberately not here: they
// are the project's own text, and composing them made the prefix diverge per
// project at the first byte of the first rule.
func Compose(base string, s *Set) string {
	block := s.BackgroundBlock()
	if block == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return block
	}
	return strings.TrimRight(base, "\n") + "\n\n" + block
}

// LoadOptions returns what this set was discovered with, so a mid-session
// reload rediscovers under the same roots and the same budgets.
func (s *Set) LoadOptions() Options {
	if s == nil {
		return Options{}
	}
	opts := s.opts
	opts.CWD, opts.UserDir = s.CWD, s.UserDir
	return opts
}

// PrefixCost is what this memory set costs in the cached system-prompt prefix:
// characters paid once per session, every session. Only pinned bodies land
// there. The fact index does not — it is reached through the `memory` tool —
// so it is not counted here; a number under this name that included it would
// be measuring the store, not the prefix.
type PrefixCost struct {
	PinnedChars int // pinned bodies folded in verbatim
	Pinned      int // pinned facts
	Budget      int // configured pinned ceiling, 0 when unset
}

// Total is the whole memory block's prefix footprint.
func (c PrefixCost) Total() int { return c.PinnedChars }

// PrefixCost measures the snapshot the session is actually running on.
func (s *Set) PrefixCost() PrefixCost {
	if s == nil {
		return PrefixCost{}
	}
	cost := PrefixCost{Pinned: len(s.PinnedGuidance), Budget: s.opts.PinnedBudgetChars}
	for _, m := range s.PinnedGuidance {
		cost.PinnedChars += utf8.RuneCountInString(strings.TrimSpace(m.Body))
	}
	return cost
}
