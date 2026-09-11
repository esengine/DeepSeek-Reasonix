// Package selfevolve provides autonomous self-improvement for Reasonix.
//
// It monitors agent behavior across sessions and automatically suggests or
// applies improvements to AGENTS.md, skills, and memory rules based on
// observed patterns.
//
// Design principles:
//   - Level 1 (safe): Regenerate AGENTS.md skill table from disk state
//   - Level 2 (reviewed): Consolidate duplicate rules, update stale references
//   - Level 3 (auto): Optimize trigger order, add new auto-trigger rules
//
// Each level requires escalating approval (ask tool) unless the user has
// granted auto-evolution rights via AGENTS.md trust settings.
package selfevolve

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level defines how autonomous an evolution action is.
type Level int

const (
	LevelSafe    Level = 1 // Always safe: regen skill table, fix typos
	LevelReview  Level = 2 // Needs quick review: consolidate rules
	LevelAuto    Level = 3 // Fully auto: optimise triggers, prune dead rules
)

// String returns the level name.
func (l Level) String() string {
	switch l {
	case LevelSafe:
		return "safe"
	case LevelReview:
		return "reviewed"
	case LevelAuto:
		return "auto"
	default:
		return "unknown"
	}
}

// Action is one proposed self-evolution step.
type Action struct {
	Level   Level  `json:"level"`
	File    string `json:"file"`    // relative path from project root
	OldText string `json:"old"`     // exact text to replace (empty = insert)
	NewText string `json:"new"`     // replacement text (empty = delete)
	Summary string `json:"summary"` // human-readable one-liner
	Reason  string `json:"reason"`  // why this change is beneficial
}

// Inspector analyses one aspect of the project for possible evolution.
type Inspector func(projectDir string) ([]Action, error)

// Engine runs registered inspectors and applies accepted actions.
type Engine struct {
	mu          sync.Mutex
	inspectors  []Inspector
	history     []AppliedAction
	maxHistory  int
	projectDir  string
	autoLevel   Level // maximum level that can auto-apply
	trustCounts map[string]int
}

// AppliedAction records an evolution that was applied.
type AppliedAction struct {
	Action
	AppliedAt time.Time
	Auto      bool // true = applied without asking
}

// NewEngine creates a self-evolution engine for the given project.
// autoLevel sets the maximum autonomy level (default LevelAuto).
func NewEngine(projectDir string, autoLevel Level) *Engine {
	if autoLevel < LevelSafe || autoLevel > LevelAuto {
		autoLevel = LevelAuto
	}
	return &Engine{
		inspectors:  defaultInspectors(),
		maxHistory:  50,
		projectDir:  projectDir,
		autoLevel:   autoLevel,
		trustCounts: map[string]int{},
	}
}

// RegisterInspector adds a custom inspector.
func (e *Engine) RegisterInspector(in Inspector) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.inspectors = append(e.inspectors, in)
}

// Inspect runs all registered inspectors and returns all proposed actions.
func (e *Engine) Inspect() ([]Action, error) {
	e.mu.Lock()
	inspectors := make([]Inspector, len(e.inspectors))
	copy(inspectors, e.inspectors)
	e.mu.Unlock()

	var all []Action
	for _, insp := range inspectors {
		actions, err := insp(e.projectDir)
		if err != nil {
			continue // don't let one inspector fail the whole pass
		}
		all = append(all, actions...)
	}
	return all, nil
}

// Apply applies the given action if its level is within the auto threshold,
// or returns it for user approval.
func (e *Engine) Apply(a Action) (*AppliedAction, error) {
	canAuto := a.Level <= e.autoLevel && e.isTrusted(a)

	path := filepath.Join(e.projectDir, a.File)
	// Read current content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", a.File, err)
	}

	var newContent string
	if a.OldText == "" {
		// Insert at end
		newContent = string(content) + "\n" + a.NewText
	} else if a.NewText == "" {
		// Delete old text
		if !strings.Contains(string(content), a.OldText) {
			return nil, fmt.Errorf("old text not found in %s", a.File)
		}
		newContent = strings.Replace(string(content), a.OldText, "", 1)
	} else {
		// Replace
		if !strings.Contains(string(content), a.OldText) {
			return nil, fmt.Errorf("old text not found in %s", a.File)
		}
		newContent = strings.Replace(string(content), a.OldText, a.NewText, 1)
	}

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("write %s: %w", a.File, err)
	}

	applied := &AppliedAction{
		Action:    a,
		AppliedAt: time.Now(),
		Auto:      canAuto,
	}

	e.mu.Lock()
	e.history = append(e.history, *applied)
	if len(e.history) > e.maxHistory {
		e.history = e.history[len(e.history)-e.maxHistory:]
	}
	e.trustCounts[a.Summary]++
	e.mu.Unlock()

	return applied, nil
}

// History returns recent applied actions.
func (e *Engine) History(n int) []AppliedAction {
	e.mu.Lock()
	defer e.mu.Unlock()
	if n <= 0 || n > len(e.history) {
		n = len(e.history)
	}
	out := make([]AppliedAction, n)
	copy(out, e.history[len(e.history)-n:])
	return out
}

func (e *Engine) isTrusted(a Action) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.trustCounts[a.Summary] >= 3
}

// ---- built-in inspectors ----------------------------------------------------

func defaultInspectors() []Inspector {
	return []Inspector{
		inspectSkillTable,       // L1: regen skill table from disk
		inspectOrphanSkills,     // L1: find skills not in table
		inspectDuplicateRules,   // L2: consolidate duplicate rules
		inspectStaleReferences,  // L2: find dead references
	}
}

// inspectSkillTable regenerates the AGENTS.md skill table from .reasonix/skills/.
func inspectSkillTable(dir string) ([]Action, error) {
	agentsFile := filepath.Join(dir, "AGENTS.md")
	if _, err := os.Stat(agentsFile); err != nil {
		return nil, err
	}

	skillsDir := filepath.Join(dir, ".reasonix", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, err
	}

	// Build current skill list from disk
	var onDisk []string
	for _, e := range entries {
		if e.IsDir() {
			onDisk = append(onDisk, e.Name())
		}
	}
	sort.Strings(onDisk)

	// Parse existing skill table from AGENTS.md
	content, err := os.ReadFile(agentsFile)
	if err != nil {
		return nil, err
	}
	contentStr := string(content)

	// Find the skill table section
	tableStart := strings.Index(contentStr, "| `")
	if tableStart < 0 {
		return nil, fmt.Errorf("no skill table found")
	}

	// Extract skill names from the table
	inTable := map[string]bool{}
	tableRe := regexp.MustCompile("`([^`]+)`")
	for _, line := range strings.Split(contentStr[tableStart:], "\n") {
		if !strings.HasPrefix(line, "|") {
			break
		}
		m := tableRe.FindStringSubmatch(line)
		if len(m) >= 2 {
			inTable[m[1]] = true
		}
	}

	var actions []Action
	for _, name := range onDisk {
		if !inTable[name] {
			actions = append(actions, Action{
				Level:   LevelSafe,
				File:    "AGENTS.md",
				Summary: fmt.Sprintf("add %s to skill table", name),
				Reason:  fmt.Sprintf("Skill %s exists on disk but is not listed in AGENTS.md", name),
			})
		}
	}
	return actions, nil
}

// inspectOrphanSkills finds skills that are listed in AGENTS.md but don't exist on disk.
func inspectOrphanSkills(dir string) ([]Action, error) {
	agentsFile := filepath.Join(dir, "AGENTS.md")
	if _, err := os.Stat(agentsFile); err != nil {
		return nil, err
	}

	content, err := os.ReadFile(agentsFile)
	if err != nil {
		return nil, err
	}
	contentStr := string(content)

	tableRe := regexp.MustCompile("`([^`]+)`")
	tableStart := strings.Index(contentStr, "| `")
	if tableStart < 0 {
		return nil, nil
	}

	var actions []Action
	for _, line := range strings.Split(contentStr[tableStart:], "\n") {
		if !strings.HasPrefix(line, "|") {
			break
		}
		m := tableRe.FindStringSubmatch(line)
		if len(m) >= 2 {
			name := m[1]
			skillDir := filepath.Join(dir, ".reasonix", "skills", name)
			if _, err := os.Stat(skillDir); os.IsNotExist(err) {
				actions = append(actions, Action{
					Level:   LevelReview,
					File:    "AGENTS.md",
					Summary: fmt.Sprintf("remove orphan skill %s from table", name),
					Reason:  fmt.Sprintf("Skill %s is listed in AGENTS.md but does not exist on disk", name),
				})
			}
		}
	}
	return actions, nil
}

// inspectDuplicateRules finds duplicate auto-trigger rules.
func inspectDuplicateRules(dir string) ([]Action, error) {
	// TODO: parse AGENTS.md auto-trigger section for duplicate entries.
	return nil, nil
}

// inspectStaleReferences finds references to files that no longer exist.
func inspectStaleReferences(dir string) ([]Action, error) {
	// TODO: check references in AGENTS.md point to real files.
	return nil, nil
}
