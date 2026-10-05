package control

import (
	"io"
	"path/filepath"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/workspaceid"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/skill"
)

// CapabilityScope names the project a capability listing belongs to. A shell
// holds several projects at once, so a settings pane that silently follows the
// active tab shows different content under an unchanged heading; every listing
// carries this so the surface can say which folder it is answering for.
type CapabilityScope struct {
	Root string `json:"root"`
	Name string `json:"name"`
	// Label disambiguates Name against the other projects on offer, so two
	// folders both called "frontend" are still distinguishable at a glance.
	Label string `json:"label,omitempty"`
	Key   string `json:"key"`
	// Repo reports that Trees working trees share these settings — the reason a
	// switch made in the main tree still applies in a worktree cut from it.
	Repo   bool   `json:"repo"`
	Trees  int    `json:"trees,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Overrides counts what this project decided for itself rather than
	// inheriting: how a picker answers "where did I change something".
	Overrides int  `json:"overrides"`
	Current   bool `json:"current,omitempty"`
}

// DescribeScope resolves one workspace root's capability identity.
func DescribeScope(root string) CapabilityScope {
	info := workspaceid.Describe(root)
	scope := CapabilityScope{
		Root:   root,
		Name:   fileutil.RootName(root),
		Key:    info.Key,
		Repo:   info.RepoDir != "",
		Trees:  info.Trees,
		Branch: info.Branch,
	}
	if rows, err := config.DefaultActivationStore().ProjectOverrides(root); err == nil {
		scope.Overrides = len(rows)
	}
	return scope
}

// CapabilityScope describes the workspace this controller's listings answer for.
func (c *Controller) CapabilityScope() CapabilityScope {
	scope := DescribeScope(c.workspaceRoot)
	scope.Current = true
	return scope
}

// DescribeScopes resolves every root a picker may offer, dropping duplicates
// and folders that resolve to one repository — nine worktrees are one project,
// and listing them nine times is the crowding this collapses.
func DescribeScopes(roots []string, current string) []CapabilityScope {
	out := make([]CapabilityScope, 0, len(roots)+1)
	seen := map[string]bool{}
	for _, root := range append([]string{current}, roots...) {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		scope := DescribeScope(root)
		if scope.Key == "" || seen[scope.Key] {
			continue
		}
		seen[scope.Key] = true
		scope.Current = root == current
		out = append(out, scope)
	}
	labelScopes(out)
	return out
}

// labelScopes gives each scope the shortest trailing path that tells it apart
// from the others. Folders with unique names keep the bare name; only a
// collision pays for extra segments, and then only as many as it takes.
func labelScopes(scopes []CapabilityScope) {
	byName := map[string][]int{}
	for i, scope := range scopes {
		byName[scope.Name] = append(byName[scope.Name], i)
	}
	for _, group := range byName {
		if len(group) < 2 {
			continue
		}
		labels := make([]string, len(group))
		for depth := 2; ; depth++ {
			whole := true
			for i, idx := range group {
				labels[i] = tailSegments(scopes[idx].Root, depth)
				whole = whole && labels[i] == filepath.ToSlash(scopes[idx].Root)
			}
			// Distinct, or every label is already the whole path and deeper
			// cannot change anything. Roots are deduplicated, so this terminates.
			if !hasDuplicate(labels) || whole {
				break
			}
		}
		for i, idx := range group {
			scopes[idx].Label = labels[i]
		}
	}
}

func tailSegments(path string, depth int) string {
	parts := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	if len(parts) <= depth {
		return filepath.ToSlash(path)
	}
	return strings.Join(parts[len(parts)-depth:], "/")
}

func hasDuplicate(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

// ProjectSkill is one skill as another project's listing shows it: what is
// declared, and whether this project has it on.
type ProjectSkill struct {
	Skill       skill.Skill
	Enabled     bool
	SwitchScope config.ActivationScope
	HasOverride bool
}

// ProjectCapabilities is a project other than the running one. Live connection
// state is absent by nature — only the session pointed at a folder can know
// whether its servers actually came up — so this answers what is declared and
// what is switched on, which is what a switch needs.
type ProjectCapabilities struct {
	Scope   CapabilityScope
	Servers []MCPServerState
	Skills  []ProjectSkill
}

// InspectProject reads one project's capabilities without repointing anything,
// so the settings surface can manage a folder the session is not sitting in.
func InspectProject(root string) ProjectCapabilities {
	out := ProjectCapabilities{Scope: DescribeScope(root)}
	cfg, err := config.LoadForRootReadOnly(root)
	if err != nil {
		return out
	}
	store := config.DefaultActivationStore()

	local := map[string]bool{}
	if rows, rowErr := store.ProjectOverrides(root); rowErr == nil {
		for _, row := range rows {
			if row.Kind == config.CapabilityMCP {
				local[row.Name] = true
			}
		}
	}
	out.Servers = make([]MCPServerState, 0, len(cfg.Plugins))
	for _, entry := range cfg.Plugins {
		enabled, resolveErr := store.IsEnabled(entry, root)
		if resolveErr != nil {
			enabled = config.DeclaredDefaultOn(entry)
		}
		state := MCPServerState{
			Entry: entry, Enabled: enabled, LocalOverride: local[entry.Name],
			Pending: store.AwaitingDecision(entry, root),
		}
		state.Description, state.Tools, state.Stale = mcpCachedFacts(mcpIdentitySpec(entry, root))
		out.Servers = append(out.Servers, state)
	}

	discovered := skill.New(skill.Options{
		ProjectRoot: root, CustomPaths: cfg.SkillCustomPaths(),
		PluginPaths: cfg.PluginPackageSkillOwners(), PluginAgentPaths: cfg.PluginPackageAgentOwners(),
		ExcludedPaths: cfg.SkillExcludedPaths(), MaxDepth: cfg.SkillMaxDepth(), Stderr: io.Discard,
	}).List()
	resolver, resolverErr := store.SkillResolverFor(root)
	out.Skills = make([]ProjectSkill, 0, len(discovered))
	for _, sk := range discovered {
		declared := !cfg.IsSkillDisabled(sk.Name)
		row := ProjectSkill{Skill: sk, Enabled: declared}
		if resolverErr == nil {
			row.Enabled = resolver.Enabled(sk.Name, declared)
			row.SwitchScope, row.HasOverride = resolver.Scope(sk.Name)
		}
		out.Skills = append(out.Skills, row)
	}
	return out
}
