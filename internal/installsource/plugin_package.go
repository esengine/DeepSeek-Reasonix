package installsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/mcpcatalog"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

const (
	claudeMarketplaceManifest = ".claude-plugin/marketplace.json"
	maxMarketplacePlugins     = 64
)

type claudeMarketplace struct {
	Name     string `json:"name"`
	Metadata struct {
		PluginRoot string `json:"pluginRoot"`
	} `json:"metadata"`
	Plugins []struct {
		Name   string          `json:"name"`
		Source json.RawMessage `json:"source"`
	} `json:"plugins"`
}

type claudeMarketplaceURLSource struct {
	Source string `json:"source"`
	URL    string `json:"url"`
	SHA    string `json:"sha"`
}

var fullGitSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func (t *installSourceTool) localPluginPackageAction(req request, root string) (action, []string, error) {
	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err != nil {
		return action{}, warnings, newErr(ErrManifestMissing, "%v", err)
	}
	act, err := t.pluginPackageAction(req, pkg, root)
	return act, warnings, err
}

func (t *installSourceTool) planGitHubPluginPackage(ctx context.Context, req request) ([]action, []string, error) {
	src, ok := parseGitHubRepoSource(req.Source)
	if !ok {
		return nil, nil, newErr(ErrUnsupportedKind, "plugin URL %q is not a GitHub repository", req.Source)
	}
	// Plan against the same source tree apply will install (a shallow clone
	// via pluginSource). A manifest-only fetch cannot see conventional
	// capability directories (skills/, commands/) or their warnings, so it
	// under-reports the capability set — and the plan the user approves must
	// describe exactly what apply installs.
	root, commit, cleanup, err := t.pluginSource(ctx, req.Source, modeForPlugin(req.Mode))
	if err != nil {
		return nil, nil, err
	}
	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err == nil {
		defer cleanup()
		act, actionErr := t.pluginPackageAction(req, pkg, req.Source)
		if actionErr != nil {
			return nil, warnings, actionErr
		}
		act.Source = req.Source
		// The commit joins the action and therefore the plan ID, so the approval
		// fingerprints the exact snapshot; apply pins to it.
		act.Commit = commit
		return []action{act}, warnings, nil
	}

	actions, marketplaceWarnings, marketplaceErr := t.planClaudeMarketplace(ctx, req, src, root, commit)
	warnings = append(warnings, marketplaceWarnings...)
	if marketplaceErr != nil {
		cleanup()
		if errors.Is(marketplaceErr, ErrNoCompatibleCapabilities) {
			return nil, warnings, marketplaceErr
		}
		return nil, warnings, newErr(ErrManifestMissing, "no plugin manifest or supported Claude marketplace found in GitHub repository %s/%s: plugin: %v; marketplace: %v", src.Owner, src.Repo, err, marketplaceErr)
	}
	if req.Apply {
		// All marketplace entries come from this one immutable clone. Reusing it
		// keeps a 12-plugin marketplace at one clone during apply and guarantees
		// every copied plugin is the snapshot represented by act.Commit.
		previousCleanup := actions[0].cleanup
		actions[0].cleanup = func() {
			if previousCleanup != nil {
				previousCleanup()
			}
			cleanup()
		}
		return actions, warnings, nil
	}
	cleanup()
	return actions, warnings, nil
}

func (t *installSourceTool) planClaudeMarketplace(ctx context.Context, req request, src githubRepoSource, root, commit string) ([]action, []string, error) {
	manifestPath := filepath.Join(root, filepath.FromSlash(claudeMarketplaceManifest))
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil, err
	}
	var marketplace claudeMarketplace
	if err := json.Unmarshal(body, &marketplace); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", claudeMarketplaceManifest, err)
	}
	if strings.TrimSpace(marketplace.Name) == "" {
		return nil, nil, fmt.Errorf("%s has no marketplace name", claudeMarketplaceManifest)
	}
	if len(marketplace.Plugins) == 0 {
		return nil, nil, fmt.Errorf("%s contains no plugins", claudeMarketplaceManifest)
	}
	if len(marketplace.Plugins) > maxMarketplacePlugins {
		return nil, nil, fmt.Errorf("%s contains %d plugins; limit is %d", claudeMarketplaceManifest, len(marketplace.Plugins), maxMarketplacePlugins)
	}

	branch := strings.TrimSpace(src.Branch)
	if branch == "" {
		branch = currentPluginGitBranch(ctx, root)
	}
	if branch == "" {
		branch = src.branches()[0]
	}

	selected := strings.TrimSpace(req.Name)
	foundSelected := selected == ""
	seen := make(map[string]bool, len(marketplace.Plugins))
	var actions []action
	keepActionResources := false
	defer func() {
		if !keepActionResources {
			cleanupActionResources(actions)
		}
	}()
	var warnings []string
	for _, entry := range marketplace.Plugins {
		entryName := strings.TrimSpace(entry.Name)
		if selected != "" && entryName != selected {
			continue
		}
		foundSelected = true
		if entryName == "" {
			warnings = append(warnings, "skipped Claude marketplace entry with an empty name")
			continue
		}
		// Validate the name at plan time so a broken entry surfaces in the
		// preview instead of failing its action mid-apply.
		if !pluginpkg.IsValidName(entryName) {
			if selected != "" {
				return nil, warnings, fmt.Errorf("marketplace plugin %q is not a valid plugin name", entryName)
			}
			warnings = append(warnings, fmt.Sprintf("skipped Claude marketplace plugin %q: not a valid plugin name", entryName))
			continue
		}
		if seen[entryName] {
			return nil, warnings, fmt.Errorf("%s contains duplicate plugin name %q", claudeMarketplaceManifest, entryName)
		}
		seen[entryName] = true

		var source string
		var pluginRoot, pluginSource, actionCommit string
		var entryCleanup func()
		if err := json.Unmarshal(entry.Source, &source); err == nil {
			if marketplaceSourceIsExternal(source) {
				if selected != "" {
					return nil, warnings, fmt.Errorf("marketplace plugin %q: external source %q must use a pinned URL object", entryName, source)
				}
				warnings = append(warnings, fmt.Sprintf("skipped Claude marketplace plugin %q: external source %q must use a pinned URL object", entryName, source))
				continue
			}
			rel, relErr := claudeMarketplaceRelativePath(marketplace.Metadata.PluginRoot, source)
			if relErr != nil {
				return nil, warnings, fmt.Errorf("plugin %q: %w", entryName, relErr)
			}
			pluginRoot = filepath.Join(root, filepath.FromSlash(rel))
			repoPath := joinURLPath(src.Path, rel)
			pluginSource = fmt.Sprintf("https://github.com/%s/%s/tree/%s/%s", src.Owner, src.Repo, branch, repoPath)
			actionCommit = commit
		} else {
			var pinned claudeMarketplaceURLSource
			if objectErr := json.Unmarshal(entry.Source, &pinned); objectErr != nil || pinned.Source != "url" || !fullGitSHA.MatchString(strings.TrimSpace(pinned.SHA)) {
				if selected != "" {
					return nil, warnings, fmt.Errorf("marketplace plugin %q: object source requires source=url, a GitHub URL, and a full 40-character SHA", entryName)
				}
				warnings = append(warnings, fmt.Sprintf("skipped Claude marketplace plugin %q: object source is not a pinned GitHub URL", entryName))
				continue
			}
			if _, ok := parseGitHubRepoSource(strings.TrimSpace(pinned.URL)); !ok {
				if selected != "" {
					return nil, warnings, fmt.Errorf("marketplace plugin %q: pinned URL %q is not a GitHub repository", entryName, pinned.URL)
				}
				warnings = append(warnings, fmt.Sprintf("skipped Claude marketplace plugin %q: pinned URL is not a GitHub repository", entryName))
				continue
			}
			var resolvedCommit string
			pluginRoot, resolvedCommit, entryCleanup, err = t.pluginSource(ctx, pinned.URL, "copy")
			if err != nil {
				return nil, warnings, fmt.Errorf("marketplace plugin %q: %w", entryName, err)
			}
			if !strings.EqualFold(resolvedCommit, pinned.SHA) {
				if err := checkoutPluginCommit(ctx, pluginRoot, pinned.SHA); err != nil {
					entryCleanup()
					return nil, warnings, fmt.Errorf("marketplace plugin %q: %w", entryName, err)
				}
			}
			pluginSource, actionCommit = strings.TrimSpace(pinned.URL), strings.ToLower(strings.TrimSpace(pinned.SHA))
		}
		pkg, pkgWarnings, err := pluginpkg.ParseDir(pluginRoot)
		warnings = append(warnings, pkgWarnings...)
		if err != nil {
			if entryCleanup != nil {
				entryCleanup()
			}
			return nil, warnings, fmt.Errorf("plugin %q: %w", entryName, err)
		}
		if pkg.Manifest.Name != entryName {
			if entryCleanup != nil {
				entryCleanup()
			}
			return nil, warnings, fmt.Errorf("marketplace plugin %q points to manifest named %q", entryName, pkg.Manifest.Name)
		}
		actionReq := req
		actionReq.Name = ""
		act, actionErr := t.pluginPackageAction(actionReq, pkg, pluginSource)
		if actionErr != nil {
			if entryCleanup != nil {
				entryCleanup()
			}
			return nil, warnings, actionErr
		}
		act.Source = pluginSource
		act.Commit = actionCommit
		act.preparedRoot = pluginRoot
		if entryCleanup != nil {
			if req.Apply {
				act.cleanup = entryCleanup
			} else {
				entryCleanup()
				act.preparedRoot = ""
			}
		}
		actions = append(actions, act)
	}
	if !foundSelected {
		return nil, warnings, fmt.Errorf("%s does not contain plugin %q", claudeMarketplaceManifest, selected)
	}
	if len(actions) == 0 {
		return nil, warnings, fmt.Errorf("%s contains no supported plugins", claudeMarketplaceManifest)
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Name < actions[j].Name })
	sort.Strings(warnings)
	warnings = slices.Compact(warnings)
	keepActionResources = true
	return actions, warnings, nil
}

func claudeMarketplaceRelativePath(pluginRoot, source string) (string, error) {
	pluginRoot = strings.TrimSpace(pluginRoot)
	if pluginRoot == "" {
		pluginRoot = "."
	}
	cleanRoot, err := cleanMarketplaceRelPath("metadata.pluginRoot", pluginRoot)
	if err != nil {
		return "", err
	}
	cleanSource, err := cleanMarketplaceRelPath("source", source)
	if err != nil {
		return "", err
	}
	rel := filepath.Clean(filepath.Join(cleanRoot, cleanSource))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source %q escapes or does not identify a plugin subdirectory", source)
	}
	return filepath.ToSlash(rel), nil
}

// cleanMarketplaceRelPath normalizes one relative-path field of a marketplace
// entry. Real marketplaces spell paths both as "./plugins/example" and as the
// bare "plugins/example", so both are accepted; absolute and drive-qualified
// paths are rejected before the join so they can never re-anchor the lookup
// outside the clone.
func cleanMarketplaceRelPath(label, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is empty", label)
	}
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(value, "./")))
	if filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" {
		return "", fmt.Errorf("%s %q must be a relative path inside the marketplace repository", label, value)
	}
	return cleaned, nil
}

// marketplaceSourceIsExternal reports whether a string source points outside
// the marketplace repository (a URL or scp-like git address) rather than at a
// relative path inside it.
func marketplaceSourceIsExternal(source string) bool {
	source = strings.TrimSpace(source)
	return strings.Contains(source, "://") || strings.HasPrefix(source, "git@")
}

func currentPluginGitBranch(ctx context.Context, root string) string {
	cmd := pluginGitCommand(ctx, "-C", root, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// pluginSource resolves a plugin source to an on-disk tree. Both the plan and
// apply phases go through this single function so their views can never
// diverge (the approval-contract guarantee); for git sources it also reports
// the resolved commit SHA ("" for local directories).
func (t *installSourceTool) pluginSource(ctx context.Context, source, mode string) (string, string, func(), error) {
	if t.preparePlugin != nil {
		return t.preparePlugin(ctx, source, mode)
	}
	return t.preparePluginSource(ctx, source, mode)
}

func (t *installSourceTool) pluginPackageAction(req request, pkg pluginpkg.Package, source string) (action, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = pkg.Manifest.Name
	}
	root := ""
	if t.reasonixHome != "" {
		root = pluginpkg.InstallRoot(t.reasonixHome, name)
	}
	skills, commands, hooks, mcp := pkg.CapabilityCounts()
	agents := pkg.Inventory().Agents
	if pkg.ManifestKind != "reasonix" && skills+commands+hooks+mcp+len(agents) == 0 {
		return action{}, newErr(ErrNoCompatibleCapabilities, "plugin %q has no Reasonix-compatible capabilities; skipped: %v", name, pkg.Compatibility.Skipped)
	}
	agentNames := make([]string, 0, len(agents))
	for _, agent := range agents {
		agentNames = append(agentNames, agent.Name)
	}
	a := action{
		Kind:                "plugin",
		Action:              "install_plugin_package",
		Name:                name,
		Source:              source,
		Target:              root,
		Scope:               "global",
		Mode:                modeForPlugin(req.Mode),
		ConfigPath:          pluginpkg.StatePath(t.reasonixHome),
		Skills:              pkg.Manifest.Skills,
		SkillCount:          skills,
		Agents:              agentNames,
		AgentCount:          len(agentNames),
		Commands:            pkg.Manifest.Commands,
		CommandCount:        commands,
		ManifestKind:        pkg.ManifestKind,
		HookCount:           hooks,
		ToolCount:           mcp,
		Compatibility:       pkg.Compatibility.Status,
		MappedCapabilities:  append([]string(nil), pkg.Compatibility.Mapped...),
		SkippedCapabilities: append([]pluginpkg.CompatibilityIssue(nil), pkg.Compatibility.Skipped...),
		Version:             pkg.Manifest.Version,
		RiskLevel:           RiskMedium,
		RiskReasons:         []string{"installs a plugin package that can add skills, commands, hooks, and MCP servers"},
	}
	if a.Mode == "link" {
		a.RiskReasons = append(a.RiskReasons, "links a plugin package from a mutable local directory")
	}
	if hooks > 0 {
		a.RiskLevel = RiskHigh
		a.RiskReasons = append(a.RiskReasons, "registers shell hooks that execute during Reasonix sessions")
	}
	if mcp > 0 {
		a.RiskLevel = RiskHigh
		a.RiskReasons = append(a.RiskReasons, "adds MCP servers that can change provider-visible tool schemas")
	}
	sort.Strings(a.Skills)
	sort.Strings(a.Agents)
	return a, nil
}

func modeForPlugin(mode string) string {
	if mode == "link" {
		return "link"
	}
	return "copy"
}

func (t *installSourceTool) applyInstallPluginPackage(ctx context.Context, req request, act *action) error {
	if t.reasonixHome == "" {
		return newErr(ErrSourceUnreadable, "plugin install requires a Reasonix home directory")
	}
	if !pluginpkg.IsValidName(act.Name) {
		return newErr(ErrInvalidManifest, "invalid plugin name %q", act.Name)
	}
	target := pluginpkg.InstallRoot(t.reasonixHome, act.Name)
	sourceRoot, commit, cleanup := act.preparedRoot, act.Commit, func() {}
	if sourceRoot == "" {
		var err error
		sourceRoot, commit, cleanup, err = t.pluginSource(ctx, act.Source, act.Mode)
		if err != nil {
			return err
		}
	}
	defer cleanup()
	if act.Commit != "" && commit != act.Commit {
		// The source moved between the approved plan and this resolution; pin
		// the clone back to the approved snapshot so what installs is exactly
		// what was reviewed.
		if err := checkoutPluginCommit(ctx, sourceRoot, act.Commit); err != nil {
			return newErr(ErrApprovalDenied, "plugin source changed since the approved plan (approved commit %s, found %s) and the approved snapshot could not be restored: %v; re-run without apply to review the new plan", act.Commit, commit, err)
		}
	}
	pkg, warnings, err := pluginpkg.ParseDir(sourceRoot)
	if err != nil {
		return newErr(ErrInvalidManifest, "%v", err)
	}
	if pkg.ManifestKind != "reasonix" {
		skills, commands, hooks, mcp := pkg.CapabilityCounts()
		if skills+commands+hooks+mcp+pkg.AgentCount() == 0 {
			return newErr(ErrInvalidManifest, "plugin %q no longer has any Reasonix-compatible capabilities", act.Name)
		}
	}
	act.Warnings = append(act.Warnings, warnings...)
	if pkg.Manifest.Name != act.Name && strings.TrimSpace(req.Name) == "" {
		return newErr(ErrInvalidManifest, "planned plugin name %q but source now reports %q", act.Name, pkg.Manifest.Name)
	}
	if act.Mode == "link" {
		if !isLinkTargetSafe(sourceRoot, t.home, t.root) {
			return newErr(ErrUnsafeLinkTarget, "plugin source %s is outside %s and %s", sourceRoot, t.root, t.home)
		}
		if err := replaceSymlink(target, sourceRoot, req.Replace); err != nil {
			return err
		}
	} else {
		if err := installCopiedPlugin(pkg, sourceRoot, target, req.Replace); err != nil {
			return err
		}
	}
	installed := pluginpkg.InstalledPlugin{
		Name:         act.Name,
		Source:       act.Source,
		Root:         pluginpkg.RelativeRoot(t.reasonixHome, target),
		Version:      pkg.Manifest.Version,
		Description:  pkg.Manifest.Description,
		ManifestKind: pkg.ManifestKind,
		Enabled:      true,
		Commit:       strings.ToLower(strings.TrimSpace(commit)),
	}
	if act.Mode == "link" {
		installed.Root = sourceRoot
	} else if verification, ok := verifyInstalledPluginCatalog(ctx, installed, target, pkg.ManifestKind); ok {
		installed.Verification = verification
	}
	if err := pluginpkg.Upsert(t.reasonixHome, installed); err != nil {
		return err
	}
	act.Target = target
	act.ManifestKind = pkg.ManifestKind
	act.Version = pkg.Manifest.Version
	act.SkillCount, act.CommandCount, act.HookCount, act.ToolCount = pkg.CapabilityCounts()
	act.AgentCount = pkg.AgentCount()
	act.Compatibility = pkg.Compatibility.Status
	act.MappedCapabilities = append([]string(nil), pkg.Compatibility.Mapped...)
	act.SkippedCapabilities = append([]pluginpkg.CompatibilityIssue(nil), pkg.Compatibility.Skipped...)
	return nil
}

func verifyInstalledPluginCatalog(ctx context.Context, installed pluginpkg.InstalledPlugin, root, manifestKind string) (*pluginpkg.Verification, bool) {
	packageDigest, err := mcpcatalog.TreeSHA256(root)
	if err != nil {
		return nil, false
	}
	manifestPath := filepath.Join(root, pluginpkg.NativeManifest)
	switch manifestKind {
	case "codex":
		manifestPath = filepath.Join(root, pluginpkg.CodexManifest)
	case "claude":
		manifestPath = filepath.Join(root, pluginpkg.ClaudeManifest)
	}
	manifestBody, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, false
	}
	manifestSum := sha256.Sum256(manifestBody)
	result, err := (mcpcatalog.Loader{CacheDir: config.CacheDir()}).Load(ctx, false)
	if err != nil {
		return nil, false
	}
	entry, ok := result.Index.Match(installed.Name, installed.Version, installed.Source, installed.Commit, packageDigest)
	if !ok || !strings.EqualFold(entry.ManifestSHA256, hex.EncodeToString(manifestSum[:])) {
		return nil, false
	}
	return &pluginpkg.Verification{
		CatalogEntryID: entry.ID, Commit: installed.Commit, PackageSHA256: packageDigest,
		VerifiedAt: time.Now().UTC(), CatalogSequence: result.Index.Sequence,
	}, true
}

func (t *installSourceTool) preparePluginSource(ctx context.Context, source, mode string) (string, string, func(), error) {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "git:github.com/") {
		source = "https://github.com/" + strings.TrimPrefix(source, "git:github.com/")
	}
	if isURL(source) {
		src, ok := parseGitHubRepoSource(source)
		if !ok {
			return "", "", func() {}, newErr(ErrUnsupportedKind, "plugin URL %q is not a GitHub repository", source)
		}
		tmp, err := os.MkdirTemp("", "reasonix-plugin-*")
		if err != nil {
			return "", "", func() {}, err
		}
		cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", src.Owner, src.Repo)
		args := []string{"clone", "--depth=1"}
		if src.Branch != "" {
			args = append(args, "--branch", src.Branch)
		}
		args = append(args, cloneURL, tmp)
		cmd := pluginGitCommand(ctx, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = os.RemoveAll(tmp)
			return "", "", func() {}, newErr(ErrSourceUnreadable, "git clone failed: %v: %s", err, strings.TrimSpace(string(out)))
		}
		commit := ""
		rev := pluginGitCommand(ctx, "-C", tmp, "rev-parse", "HEAD")
		if out, err := rev.Output(); err == nil {
			commit = strings.TrimSpace(string(out))
		}
		root := tmp
		if src.Path != "" {
			root = filepath.Join(tmp, filepath.FromSlash(src.Path))
		}
		return root, commit, func() { _ = os.RemoveAll(tmp) }, nil
	}
	path := t.resolvePath(source)
	if mode == "link" {
		return path, "", func() {}, nil
	}
	return path, "", func() {}, nil
}

// verifyCopiedCapabilities re-parses the installed copy and requires its
// capability counts to match the source tree the plan described. Discovery
// follows symlinks but copy mode can only materialize links that stay inside
// the package, so an unmaterializable link would otherwise silently install
// fewer skills/commands than the approval covered.
func verifyCopiedCapabilities(src pluginpkg.Package, target string) error {
	installed, _, err := pluginpkg.ParseDir(target)
	if err != nil {
		return newErr(ErrInvalidManifest, "installed plugin tree failed to re-parse: %v", err)
	}
	ss, sc, sh, sm := src.CapabilityCounts()
	is, ic, ih, im := installed.CapabilityCounts()
	sa, ia := src.AgentCount(), installed.AgentCount()
	if ss != is || sc != ic || sh != ih || sm != im || sa != ia {
		return newErr(ErrInvalidManifest,
			"installed copy resolves to %d skills / %d agents / %d commands / %d hooks / %d MCP servers but the approved plan counted %d/%d/%d/%d/%d — the package likely uses symlinks copy mode cannot materialize safely; retry with mode=link or fix the package layout",
			is, ia, ic, ih, im, ss, sa, sc, sh, sm)
	}
	return nil
}

// checkoutPluginCommit pins a fresh clone to the approved commit when its HEAD
// has moved past it. GitHub serves full-SHA fetches, so the approved snapshot
// stays reachable after ordinary pushes; a history rewrite that discarded it
// fails here — exactly the case where the user must re-review the plan.
func checkoutPluginCommit(ctx context.Context, cloneRoot, commit string) error {
	fetch := pluginGitCommand(ctx, "-C", cloneRoot, "fetch", "--depth=1", "origin", commit)
	if out, err := fetch.CombinedOutput(); err != nil {
		return fmt.Errorf("fetch approved commit %s: %v: %s", commit, err, strings.TrimSpace(string(out)))
	}
	co := pluginGitCommand(ctx, "-C", cloneRoot, "checkout", "--detach", commit)
	if out, err := co.CombinedOutput(); err != nil {
		return fmt.Errorf("checkout approved commit %s: %v: %s", commit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func pluginGitCommand(ctx context.Context, args ...string) *exec.Cmd {
	// Preserve repository bytes across platforms. A user's global autocrlf
	// setting must not rewrite JSON/scripts on Windows and make the installed
	// tree differ from the catalog's signed package digest.
	gitArgs := append([]string{"-c", "core.autocrlf=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Env = secrets.ProcessEnv()
	proc.HideWindow(cmd)
	return cmd
}

// installCopiedPlugin copies sourceRoot into a staging directory next to
// target, verifies the staged tree resolves to the capability set the plan
// approved, and only then swaps it into place with a backup-protected rename.
// Any failure before the swap — copy error, capability mismatch — leaves an
// existing installation completely intact, so a bad update can never destroy
// the working version it was meant to replace.
func installCopiedPlugin(pkg pluginpkg.Package, sourceRoot, target string, replace bool) error {
	if _, err := os.Lstat(target); err == nil && !replace {
		return newErr(ErrAlreadyExists, "plugin package already exists at %s; retry with replace=true to update it", target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(target), "."+filepath.Base(target)+".staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := copyDir(sourceRoot, staging); err != nil {
		return err
	}
	// Fail closed when the copied tree resolves to a different capability set
	// than the plan the user approved — e.g. a symlink copyDir could not
	// materialize safely. A silent gap here would install less than reviewed.
	if err := verifyCopiedCapabilities(pkg, staging); err != nil {
		return err
	}
	if err := os.Chmod(staging, 0o755); err != nil { // MkdirTemp creates 0700
		return err
	}
	// Swap staged tree into place. The backup rename keeps the previous
	// install restorable until the new tree has landed; both renames stay on
	// one filesystem (same parent dir), so each is atomic. The backup name
	// derives from the staging dir: dot-prefixed and randomized, it can never
	// pass IsValidName, so it cannot collide with a sibling plugin's install
	// dir (plugin names may legally contain dots, e.g. "foo.pre-replace") and
	// needs no pre-cleanup that could delete such a neighbor.
	backup := staging + ".old"
	hadOld := false
	if _, err := os.Lstat(target); err == nil {
		hadOld = true
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, target); err != nil {
		if hadOld {
			_ = os.Rename(backup, target) // restore the previous install
		}
		return err
	}
	if hadOld {
		_ = os.RemoveAll(backup)
	}
	return nil
}

func replaceSymlink(target, sourceRoot string, replace bool) error {
	if _, err := os.Lstat(target); err == nil {
		if !replace {
			return newErr(ErrAlreadyExists, "plugin package already exists at %s; retry with replace=true to update it", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.Symlink(sourceRoot, target)
}

func (t *installSourceTool) applyRemovePluginPackage(_ request, act *action) error {
	installed, ok, err := pluginpkg.Remove(t.reasonixHome, act.Name)
	if err != nil || !ok {
		return err
	}
	root := pluginpkg.ResolveRoot(t.reasonixHome, installed.Root)
	if t.onDisconnect != nil {
		if pkg, _, err := pluginpkg.ParseDir(root); err == nil {
			names := make([]string, 0, len(pkg.Manifest.MCPServers))
			for name := range pkg.Manifest.MCPServers {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				t.onDisconnect(name)
			}
		}
	}
	pluginsDir := pluginpkg.PluginsDir(t.reasonixHome)
	if rel, err := filepath.Rel(pluginsDir, root); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		if err := os.RemoveAll(root); err != nil {
			return err
		}
	}
	return nil
}
