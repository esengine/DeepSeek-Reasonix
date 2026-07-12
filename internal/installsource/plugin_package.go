package installsource

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/pluginpkg"
	"reasonix/internal/secrets"
)

func (t *installSourceTool) localPluginPackageAction(req request, root string) (action, []string, error) {
	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err != nil {
		return action{}, warnings, newErr(ErrManifestMissing, "%v", err)
	}
	return t.pluginPackageAction(req, pkg, root), warnings, nil
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
	defer cleanup()
	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err != nil {
		return nil, warnings, newErr(ErrManifestMissing, "no plugin manifest found in GitHub repository %s/%s: %v", src.Owner, src.Repo, err)
	}
	act := t.pluginPackageAction(req, pkg, req.Source)
	act.Source = req.Source
	// The commit joins the action and therefore the plan ID, so the approval
	// fingerprints the exact snapshot; apply pins to it.
	act.Commit = commit
	return []action{act}, warnings, nil
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

func (t *installSourceTool) pluginPackageAction(req request, pkg pluginpkg.Package, source string) action {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = pkg.Manifest.Name
	}
	root := ""
	if t.reasonixHome != "" {
		root = pluginpkg.InstallRoot(t.reasonixHome, name)
	}
	skills, commands, hooks, mcp := pkg.CapabilityCounts()
	a := action{
		Kind:         "plugin",
		Action:       "install_plugin_package",
		Name:         name,
		Source:       source,
		Target:       root,
		Scope:        "global",
		Mode:         modeForPlugin(req.Mode),
		ConfigPath:   pluginpkg.StatePath(t.reasonixHome),
		Skills:       pkg.Manifest.Skills,
		SkillCount:   skills,
		Commands:     pkg.Manifest.Commands,
		CommandCount: commands,
		ManifestKind: pkg.ManifestKind,
		HookCount:    hooks,
		ToolCount:    mcp,
		Version:      pkg.Manifest.Version,
		RiskLevel:    RiskMedium,
		RiskReasons:  []string{"installs a plugin package that can add skills, commands, hooks, and MCP servers"},
	}
	if a.Mode == "link" {
		a.RiskReasons = append(a.RiskReasons, "links a plugin package from a mutable local directory")
	}
	if hooks > 0 {
		a.RiskReasons = append(a.RiskReasons, "registers shell hooks that execute during Reasonix sessions")
	}
	if mcp > 0 {
		a.RiskReasons = append(a.RiskReasons, "adds MCP servers that can change provider-visible tool schemas")
	}
	sort.Strings(a.Skills)
	return a
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
	sourceRoot, commit, cleanup, err := t.pluginSource(ctx, act.Source, act.Mode)
	if err != nil {
		return err
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
	}
	if act.Mode == "link" {
		installed.Root = sourceRoot
	}
	if err := pluginpkg.Upsert(t.reasonixHome, installed); err != nil {
		return err
	}
	act.Target = target
	act.ManifestKind = pkg.ManifestKind
	act.Version = pkg.Manifest.Version
	act.SkillCount, act.CommandCount, act.HookCount, act.ToolCount = pkg.CapabilityCounts()
	return nil
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
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Env = secrets.ProcessEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = os.RemoveAll(tmp)
			return "", "", func() {}, newErr(ErrSourceUnreadable, "git clone failed: %v: %s", err, strings.TrimSpace(string(out)))
		}
		commit := ""
		rev := exec.CommandContext(ctx, "git", "-C", tmp, "rev-parse", "HEAD")
		rev.Env = secrets.ProcessEnv()
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
	if ss != is || sc != ic || sh != ih || sm != im {
		return newErr(ErrInvalidManifest,
			"installed copy resolves to %d skills / %d commands / %d hooks / %d MCP servers but the approved plan counted %d/%d/%d/%d — the package likely uses symlinks copy mode cannot materialize safely; retry with mode=link or fix the package layout",
			is, ic, ih, im, ss, sc, sh, sm)
	}
	return nil
}

// checkoutPluginCommit pins a fresh clone to the approved commit when its HEAD
// has moved past it. GitHub serves full-SHA fetches, so the approved snapshot
// stays reachable after ordinary pushes; a history rewrite that discarded it
// fails here — exactly the case where the user must re-review the plan.
func checkoutPluginCommit(ctx context.Context, cloneRoot, commit string) error {
	fetch := exec.CommandContext(ctx, "git", "-C", cloneRoot, "fetch", "--depth=1", "origin", commit)
	fetch.Env = secrets.ProcessEnv()
	if out, err := fetch.CombinedOutput(); err != nil {
		return fmt.Errorf("fetch approved commit %s: %v: %s", commit, err, strings.TrimSpace(string(out)))
	}
	co := exec.CommandContext(ctx, "git", "-C", cloneRoot, "checkout", "--detach", commit)
	co.Env = secrets.ProcessEnv()
	if out, err := co.CombinedOutput(); err != nil {
		return fmt.Errorf("checkout approved commit %s: %v: %s", commit, err, strings.TrimSpace(string(out)))
	}
	return nil
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
