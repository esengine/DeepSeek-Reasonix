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
	var warnings []string
	for _, branch := range src.branches() {
		for _, manifestPath := range pluginpkg.ManifestPaths() {
			rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", src.Owner, src.Repo, branch, joinURLPath(src.Path, manifestPath))
			body, err := t.fetchText(ctx, rawURL)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %s", rawURL, err.Error()))
				continue
			}
			tmp, err := os.MkdirTemp("", "reasonix-plugin-plan-*")
			if err != nil {
				return nil, warnings, err
			}
			defer os.RemoveAll(tmp)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(tmp, manifestPath)), 0o755); err != nil {
				return nil, warnings, err
			}
			if err := os.WriteFile(filepath.Join(tmp, manifestPath), []byte(body), 0o644); err != nil {
				return nil, warnings, err
			}
			if strings.EqualFold(manifestPath, pluginpkg.CodexManifest) {
				if hookBody, err := t.fetchText(ctx, fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", src.Owner, src.Repo, branch, joinURLPath(src.Path, "hooks/session-start-codex"))); err == nil {
					hookPath := filepath.Join(tmp, "hooks", "session-start-codex")
					if mkErr := os.MkdirAll(filepath.Dir(hookPath), 0o755); mkErr == nil {
						_ = os.WriteFile(hookPath, []byte(hookBody), 0o755)
					}
				}
			}
			pkg, pkgWarnings, err := pluginpkg.ParseDir(tmp)
			warnings = append(warnings, pkgWarnings...)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %s", rawURL, err.Error()))
				continue
			}
			act := t.pluginPackageAction(req, pkg, req.Source)
			act.Source = req.Source
			return []action{act}, warnings, nil
		}
	}
	return nil, warnings, newErr(ErrManifestMissing, "no plugin manifest found in GitHub repository %s/%s", src.Owner, src.Repo)
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
	skills, hooks, mcp := pkg.CapabilityCounts()
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
		HookCount:    hooks,
		ToolCount:    mcp,
		ManifestKind: pkg.ManifestKind,
		Version:      pkg.Manifest.Version,
		RiskLevel:    RiskMedium,
		RiskReasons:  []string{"installs a plugin package that can add skills, hooks, and MCP servers"},
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
	sourceRoot, cleanup, err := t.preparePluginSource(ctx, act.Source, act.Mode)
	if err != nil {
		return err
	}
	defer cleanup()
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
		if err := replaceCopiedPlugin(sourceRoot, target, req.Replace); err != nil {
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
	act.SkillCount, act.HookCount, act.ToolCount = pkg.CapabilityCounts()
	return nil
}

func (t *installSourceTool) preparePluginSource(ctx context.Context, source, mode string) (string, func(), error) {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "git:github.com/") {
		source = "https://github.com/" + strings.TrimPrefix(source, "git:github.com/")
	}
	if isURL(source) {
		src, ok := parseGitHubRepoSource(source)
		if !ok {
			return "", func() {}, newErr(ErrUnsupportedKind, "plugin URL %q is not a GitHub repository", source)
		}
		tmp, err := os.MkdirTemp("", "reasonix-plugin-*")
		if err != nil {
			return "", func() {}, err
		}
		cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", src.Owner, src.Repo)
		args := []string{"clone", "--depth=1"}
		if src.Branch != "" {
			args = append(args, "--branch", src.Branch)
		}
		args = append(args, cloneURL, tmp)
		cmd := exec.CommandContext(ctx, "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = os.RemoveAll(tmp)
			return "", func() {}, newErr(ErrSourceUnreadable, "git clone failed: %v: %s", err, strings.TrimSpace(string(out)))
		}
		root := tmp
		if src.Path != "" {
			root = filepath.Join(tmp, filepath.FromSlash(src.Path))
		}
		return root, func() { _ = os.RemoveAll(tmp) }, nil
	}
	path := t.resolvePath(source)
	if mode == "link" {
		return path, func() {}, nil
	}
	return path, func() {}, nil
}

func replaceCopiedPlugin(sourceRoot, target string, replace bool) error {
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
	return copyDir(sourceRoot, target)
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
