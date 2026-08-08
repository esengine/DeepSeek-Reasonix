package evolve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/memory"
)

// ApplyDeps supplies disk targets for Apply. MemoryStore handles L0; Standing
// writes use StandingPath (or ProjectRoot/AGENTS.md discovery) via memory.Set.
type ApplyDeps struct {
	// MemoryStore is the auto-memory store for L0. Zero/disabled rejects L0.
	MemoryStore memory.Store
	// ProjectRoot is the workspace cwd used for standing-doc discovery.
	ProjectRoot string
	// StandingPath, when set, is the exact standing instruction file for L1.
	// When empty, Apply resolves AGENTS.md/REASONIX.md/CLAUDE.md under ProjectRoot.
	StandingPath string
	// ProposalStore, when set, persists status transitions after apply.
	ProposalStore *Store
}

// ApplyResult describes what Apply did.
type ApplyResult struct {
	Proposal Proposal
	Path     string
	Noop     bool
	TailNote string
	Diff     string
	NewBody  string // L1 full standing doc body after patch (for tests/preview)
}

// Apply validates and lands a proposal. Already-applied proposals succeed as no-ops.
// Disk is updated; callers must not reload the live session Compose prefix.
func Apply(p Proposal, deps ApplyDeps) (ApplyResult, error) {
	p.Status = NormalizeStatus(string(p.Status))
	p.Tier = NormalizeTier(string(p.Tier))
	if p.Status == StatusApplied {
		return ApplyResult{
			Proposal: p,
			Path:     p.AppliedPath,
			Noop:     true,
			TailNote: p.TailNote,
		}, nil
	}
	if err := Validate(p); err != nil {
		return ApplyResult{}, err
	}

	var (
		result ApplyResult
		err    error
	)
	switch p.Tier {
	case TierL0:
		result, err = applyL0(p, deps)
	case TierL1:
		result, err = applyL1(p, deps)
	default:
		return ApplyResult{}, fmt.Errorf("unsupported tier %q", p.Tier)
	}
	if err != nil {
		return ApplyResult{}, err
	}

	now := time.Now().UTC()
	result.Proposal.Status = StatusApplied
	result.Proposal.UpdatedAt = now
	result.Proposal.AppliedPath = result.Path
	result.Proposal.TailNote = result.TailNote
	if result.Diff != "" {
		result.Proposal.Diff = result.Diff
	}
	if deps.ProposalStore != nil && deps.ProposalStore.Dir != "" {
		if err := deps.ProposalStore.Save(result.Proposal); err != nil {
			return ApplyResult{}, fmt.Errorf("apply succeeded but failed to persist proposal state: %w", err)
		}
	}
	return result, nil
}

func applyL0(p Proposal, deps ApplyDeps) (ApplyResult, error) {
	if deps.MemoryStore.Dir == "" && deps.MemoryStore.GlobalDir == "" {
		return ApplyResult{}, fmt.Errorf("memory store unavailable")
	}
	name := strings.TrimSpace(p.Target.MemoryName)
	if name == "" {
		name = slugify(p.Title)
	}
	title := strings.TrimSpace(p.Target.MemoryTitle)
	if title == "" {
		title = oneLine(p.Title)
	}
	desc := strings.TrimSpace(p.Target.Description)
	if desc == "" {
		desc = oneLine(p.Why)
	}
	mt := strings.ToLower(strings.TrimSpace(p.Target.MemoryType))
	if mt == "" {
		mt = "feedback"
	}
	body := strings.TrimSpace(p.Body)
	if body == "" {
		body = formatL0Body(p)
	} else if !strings.Contains(body, "**Why:**") {
		// Ensure Why/How structure when callers supply free-form body.
		body = formatL0Body(p)
	}

	saved, err := deps.MemoryStore.SaveWithOptions(memory.Memory{
		Name:        name,
		Title:       title,
		Description: desc,
		Type:        memory.NormalizeType(mt),
		Scope:       memory.FactScopeProject,
		Body:        body,
	}, memory.SaveOptions{})
	if err != nil {
		return ApplyResult{}, err
	}
	tail := fmt.Sprintf("Applied evolve proposal %s (L0 memory %s). Full effect loads in the next session.", p.ID, name)
	return ApplyResult{
		Proposal: p,
		Path:     saved.Path,
		TailNote: tail,
	}, nil
}

func formatL0Body(p Proposal) string {
	var b strings.Builder
	b.WriteString("**Why:** ")
	b.WriteString(strings.TrimSpace(p.Why))
	b.WriteString("\n\n**How to apply:** ")
	b.WriteString(strings.TrimSpace(p.HowToApply))
	if len(p.Evidence) > 0 {
		e := p.Evidence[0]
		b.WriteString("\n\n**Evidence:** ")
		b.WriteString(e.SessionPath)
		b.WriteString(fmt.Sprintf(" @msg %d", e.MessageIndex))
		if q := oneLine(e.Quote); q != "" {
			b.WriteString(" — ")
			b.WriteString(q)
		}
	}
	if extra := strings.TrimSpace(p.Body); extra != "" && !strings.Contains(extra, "**Why:**") {
		b.WriteString("\n\n")
		b.WriteString(extra)
	}
	return b.String()
}

func applyL1(p Proposal, deps ApplyDeps) (ApplyResult, error) {
	path, err := resolveStandingPath(deps)
	if err != nil {
		return ApplyResult{}, err
	}
	oldRaw, _ := fileencoding.ReadFileUTF8(path)
	oldBody := string(oldRaw)
	bullet := strings.TrimSpace(p.Body)
	if bullet == "" {
		bullet = composeL1Bullet(p.Title, p.HowToApply)
	} else if !strings.HasPrefix(strings.TrimSpace(bullet), "-") {
		bullet = composeL1Bullet(p.Title, bullet)
	}
	if lineCount(bullet) > MaxL1BodyLines {
		return ApplyResult{}, fmt.Errorf("L1 body exceeds %d lines", MaxL1BodyLines)
	}

	newBody, change := PatchStandingDoc(path, oldBody, bullet)
	if newBody == oldBody || (strings.TrimSpace(oldBody) != "" && change.Added == 0 && change.Removed == 0 && oldBody == newBody) {
		// Identical bullet already present — still mark applied.
		tail := fmt.Sprintf("Applied evolve proposal %s (L1 standing doc already contained the rule). Full effect loads in the next session.", p.ID)
		return ApplyResult{
			Proposal: p,
			Path:     path,
			Noop:     true,
			TailNote: tail,
			Diff:     change.Diff,
			NewBody:  newBody,
		}, nil
	}

	// Write through memory.Set so allowlisting stays consistent with panel paths.
	set := standingWriteSet(deps.ProjectRoot, path)
	if _, err := set.WriteDoc(path, newBody); err != nil {
		return ApplyResult{}, err
	}
	tail := fmt.Sprintf("Applied evolve proposal %s (L1 standing doc %s). Full effect loads in the next session.", p.ID, filepath.Base(path))
	return ApplyResult{
		Proposal: p,
		Path:     path,
		TailNote: tail,
		Diff:     change.Diff,
		NewBody:  newBody,
	}, nil
}

func resolveStandingPath(deps ApplyDeps) (string, error) {
	if p := strings.TrimSpace(deps.StandingPath); p != "" {
		return p, nil
	}
	root := strings.TrimSpace(deps.ProjectRoot)
	if root == "" {
		return "", fmt.Errorf("project root or standing path is required for L1")
	}
	// Prefer an existing convention file; default AGENTS.md for create.
	for _, name := range []string{"REASONIX.md", "AGENTS.md", "CLAUDE.md"} {
		cand := filepath.Join(root, name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return filepath.Join(root, "AGENTS.md"), nil
}

func standingWriteSet(projectRoot, standingPath string) *memory.Set {
	cwd := projectRoot
	if cwd == "" {
		cwd = filepath.Dir(standingPath)
	}
	return &memory.Set{
		CWD: cwd,
		Docs: []memory.Source{{
			Path:      standingPath,
			Scope:     memory.ScopeProject,
			Directory: cwd,
		}},
	}
}
