// Package memory implements Reasonix's persistent memory. It mirrors Claude
// Code's two-layer model while honoring Reasonix's cache-first architecture:
//
//   - Standing instructions resolved by internal/instruction and exposed here
//     through compatibility aliases for existing panel and controller APIs.
//   - Auto-memory store: per-project fact files with frontmatter plus a MEMORY.md
//     index, which the model maintains via the `remember` tool (see store.go).
//
// All of it folds into the durable system-prompt prefix exactly once at boot
// (see Compose), so it rides DeepSeek's automatic prefix cache at zero per-turn
// cost. Mid-session changes never mutate that prefix; they take effect through
// the controller's transient tail-injection and fold into the prefix on the next
// session.
package memory

import (
	"path/filepath"

	"reasonix/internal/instruction"
)

// Scope labels where a doc source was discovered, so the assembled block can
// attribute each chunk and callers (e.g. the `#` quick-add picker) can offer
// meaningful targets.
type Scope = instruction.Scope

const (
	ScopeUser     = instruction.ScopeUser     // ~/.reasonix/REASONIX.md
	ScopeAncestor = instruction.ScopeAncestor // an instruction file between workspace root and target
	ScopeProject  = instruction.ScopeProject  // instruction file at the workspace root
	ScopeLocal    = instruction.ScopeLocal    // *.local.md personal override
)

// docNames are the recognized memory filenames at each level, in load order.
// REASONIX.md is ours; AGENTS.md and CLAUDE.md are the cross-tool conventions.
// When several distinct files exist in one directory, all load (each labeled with
// its source path), so a repo already carrying an AGENTS.md / CLAUDE.md is picked
// up without renaming. New docs are created as AGENTS.md (the universal
// convention) — see defaultDocName / Set.DocPath.
var docNames = instruction.DocumentNames

// localNames are the personal, git-ignored overrides, highest precedence.
var localNames = instruction.LocalDocumentNames

// defaultDocName / defaultLocalName are the filenames a fresh doc is created as
// when a directory has none yet: AGENTS.md is the widely-shared convention, so a
// new project's memory is portable to other agent tools out of the box.
const (
	defaultDocName   = "AGENTS.md"
	defaultLocalName = "AGENTS.local.md"
)

// Source is one loaded memory file with provenance and @import-expanded body.
type Source = instruction.Document

// absOf returns the absolute form of p, falling back to a cleaned p on error so
// the value is still usable as a stable map key.
func absOf(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

// sameDir reports whether two paths denote the same directory.
func sameDir(a, b string) bool { return absOf(a) == absOf(b) }
