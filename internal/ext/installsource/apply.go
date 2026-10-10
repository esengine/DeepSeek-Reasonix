package installsource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/skill"
)

// apply dispatches to the per-action implementation. Each branch is
// responsible for setting act.Status / act.Error / act.Next and for
// cleaning up any partial side effects it left behind.
func (t *Tool) apply(ctx context.Context, req request, act *action) error {
	switch act.Kind {
	case "skill":
		switch act.Action {
		case "register_skill_root":
			return t.applySkillRoot(req, act)
		case "copy_skill":
			return t.applyCopySkill(req, act)
		case "link_skill":
			return t.applyLinkSkill(req, act)
		case "remove_skill":
			return t.applyRemoveSkill(req, act)
		case "remove_skill_root":
			return t.applyRemoveSkillRoot(req, act)
		default:
			return fmt.Errorf("unknown skill action %q", act.Action)
		}
	case "mcp":
		switch act.Action {
		case "install_mcp_server":
			return t.applyInstallMCP(ctx, req, act)
		case "remove_mcp_server":
			return t.applyRemoveMCP(req, act)
		default:
			return fmt.Errorf("unknown mcp action %q", act.Action)
		}
	case "plugin":
		switch act.Action {
		case "install_plugin_package":
			return t.applyInstallPluginPackage(ctx, req, act)
		case "remove_plugin_package":
			return t.applyRemovePluginPackage(req, act)
		default:
			return fmt.Errorf("unknown plugin action %q", act.Action)
		}
	default:
		return fmt.Errorf("unknown install action kind %q", act.Kind)
	}
}

// applySkillRoot appends the path to the active config's [skills].paths and
// re-builds the Store to confirm the listed skills are discoverable.
func (t *Tool) applySkillRoot(req request, act *action) error {
	var cfg *config.Config
	if err := config.EditConfigFile(act.ConfigPath, func(fresh *config.Config) error {
		if err := fresh.AddSkillPath(act.Source); err != nil {
			return err
		}
		cfg = fresh
		return nil
	}); err != nil {
		return err
	}
	store := skill.New(skill.Options{HomeDir: t.home, ReasonixHomeDir: t.reasonixHome, ProjectRoot: t.root, CustomPaths: append(cfg.SkillCustomPaths(), act.Source)})
	for _, name := range act.Skills {
		sk, ok := store.Read(name)
		if !ok {
			return newErr(ErrSourceUnreadable, "skill %q was registered but is not discoverable", name)
		}
		var registeredPath string
		for _, file := range act.skillFiles[name] {
			if config.CanonicalSkillPath(file) == config.CanonicalSkillPath(sk.Path) {
				registeredPath = file
				break
			}
		}
		if registeredPath == "" {
			act.Warnings = append(act.Warnings, fmt.Sprintf("skill %q registered from %s is not selected in this workspace; current selection is %s", name, act.Source, sk.Path))
			continue
		}
		act.Discoverable = true
		if act.CanonicalPath == "" {
			act.CanonicalPath = registeredPath
		}
		if strings.TrimSpace(sk.Description) == "" {
			act.Warnings = append(act.Warnings, fmt.Sprintf("skill %q has no description frontmatter; it is installed but the skills index will use a placeholder", name))
		}
	}
	for _, listed := range store.List() {
		for _, file := range act.skillFiles[listed.Name] {
			if config.CanonicalSkillPath(file) == config.CanonicalSkillPath(listed.Path) {
				act.Indexed = true
				break
			}
		}
	}
	act.Target = act.Source
	return nil
}

// applyCopySkill copies a single skill into the project/global skills dir.
// We refuse to overwrite any existing canonical directory or legacy flat file.
// copyDir uses O_EXCL so any race that slips through the Lstat check still
// loses atomically.
func (t *Tool) applyCopySkill(req request, act *action) error {
	canonical, err := t.skillCanonicalPath(act.skill.Name, act.Scope)
	if err != nil {
		return err
	}
	targetDir := filepath.Dir(canonical)
	conflicts, err := t.skillConflictTargets(act.skill.Name, act.Scope)
	if err != nil {
		return err
	}
	for _, conflict := range conflicts {
		if _, err := os.Lstat(conflict); err == nil {
			return newErr(ErrAlreadyExists, "skill %q already exists at %s", act.skill.Name, conflict)
		}
	}
	if act.skill.IsDir {
		if err := copyDir(act.skill.SourcePath, targetDir, maxSkillCopyBytes); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return err
		}
		if err := writeNewFile(canonical, []byte(act.skill.Content)); err != nil {
			return err
		}
	}
	act.Target = canonical
	act.CanonicalPath = canonical
	return t.verifySkill(act.Scope, act.skill.Name, act)
}

// applyLinkSkill creates a symlink in the skills dir pointing at the source.
// Absolute sources outside the project or home root are blocked even when the
// plan was approved: a link-mode skill should not become a backdoor to arbitrary
// host files.
func (t *Tool) applyLinkSkill(req request, act *action) error {
	canonical, err := t.skillCanonicalPath(act.skill.Name, act.Scope)
	if err != nil {
		return err
	}
	target := canonical
	if act.skill.IsDir {
		target = filepath.Dir(canonical)
	}
	conflicts, err := t.skillConflictTargets(act.skill.Name, act.Scope)
	if err != nil {
		return err
	}
	for _, conflict := range conflicts {
		if _, err := os.Lstat(conflict); err == nil {
			return newErr(ErrAlreadyExists, "skill %q already exists at %s", act.skill.Name, conflict)
		}
	}
	source, err := filepath.EvalSymlinks(act.skill.SourcePath)
	if err != nil {
		return newErr(ErrSourceUnreadable, "%v", err)
	}
	if !isLinkTargetSafe(source, t.home, t.root) {
		act.RiskLevel = RiskHigh
		act.RiskReasons = append(act.RiskReasons, "link target is an absolute path outside the project or home root")
		return newErr(ErrUnsafeLinkTarget, "skill %q source %s is outside %s and %s", act.skill.Name, act.skill.SourcePath, t.root, t.home)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(source, target); err != nil {
		return err
	}
	act.Target = target
	act.CanonicalPath = canonical
	return t.verifySkill(act.Scope, act.skill.Name, act)
}

// isLinkTargetSafe allows relative sources or sources within home/project roots.
// Existing directory aliases are resolved on both sides of the comparison;
// unresolved sources retain the lexical comparison used for planning.
func isLinkTargetSafe(source, home, projectRoot string) bool {
	if source == "" {
		return false
	}
	if !filepath.IsAbs(source) {
		return true
	}
	clean := filepath.Clean(source)
	resolved, resolveErr := filepath.EvalSymlinks(clean)
	if resolveErr == nil {
		clean = resolved
	}
	for _, root := range []string{home, projectRoot} {
		if root == "" {
			continue
		}
		base := filepath.Clean(root)
		if resolved, err := filepath.EvalSymlinks(base); resolveErr == nil && err == nil {
			base = resolved
		}
		if clean == base {
			return true
		}
		if strings.HasPrefix(clean, base+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// applyInstallMCP connects an MCP server and persists its config. The order
// is deliberate: connect first (so the user can use the tools immediately),
// then SaveTo (so a persistence failure is detectable). If SaveTo fails, we
// roll back the connection and any tools the caller already registered, so
// the live session is not out of sync with the on-disk config.
func (t *Tool) applyInstallMCP(ctx context.Context, req request, act *action) error {
	if act.entry.Name == "" {
		return newErr(ErrInvalidManifest, "MCP action has no server entry")
	}
	cfg, err := config.LoadForEditReadOnlyStrict(act.ConfigPath)
	if err != nil {
		return err
	}
	var previous config.PluginEntry
	hadPrevious := false
	for _, existing := range cfg.Plugins {
		if existing.Name == act.entry.Name {
			previous = existing
			if act.Scope == "project" {
				previous.Source = config.MCPSourceProjectConfig
			} else {
				previous.Source = config.MCPSourceUserConfig
			}
			hadPrevious = true
			break
		}
	}
	if !req.Replace {
		if hadPrevious {
			return newErr(ErrAlreadyExists, "MCP server %q already exists in %s; retry with replace=true to update it", act.entry.Name, act.ConfigPath)
		}
	}

	var connected bool
	oldDisconnected := false
	if req.Replace && hadPrevious && t.onDisconnect != nil {
		oldDisconnected = t.onDisconnect(act.entry.Name)
	}
	if t.connectMCP != nil {
		res, err := t.connectMCP(act.entry)
		if err != nil {
			if oldDisconnected {
				if rbErr := t.restoreMCP(previous); rbErr != nil {
					return fmt.Errorf("%w; reconnect previous server failed: %w", err, rbErr)
				}
			}
			return err
		}
		act.ToolCount = res.ToolCount
		connected = res.Disconnect != nil || res.ToolCount >= 0
		// Stash the disconnect on the action so a later SaveTo failure can
		// undo the connect.
		act.disconnect = res.Disconnect
	}
	probe := config.Default()
	if err := probe.UpsertPlugin(act.entry); err != nil {
		if rbErr := t.rollbackMCPReplace(act, previous, oldDisconnected, connected); rbErr != nil {
			return fmt.Errorf("%w; rollback failed: %w", err, rbErr)
		}
		return err
	}
	if err := config.EditConfigFile(act.ConfigPath, func(fresh *config.Config) error {
		current, currentFound := pluginEntryNamed(fresh.Plugins, act.entry.Name, act.Scope)
		switch {
		case !req.Replace && currentFound:
			return newErr(ErrAlreadyExists, "MCP server %q already exists in %s; retry with replace=true to update it", act.entry.Name, act.ConfigPath)
		case req.Replace && currentFound != hadPrevious:
			return fmt.Errorf("MCP server %q changed while it was connecting", act.entry.Name)
		case req.Replace && currentFound && !reflect.DeepEqual(current, previous):
			return fmt.Errorf("MCP server %q changed while it was connecting", act.entry.Name)
		}
		return fresh.UpsertPlugin(act.entry)
	}); err != nil {
		if rbErr := t.rollbackMCPReplace(act, previous, oldDisconnected, connected); rbErr != nil {
			return fmt.Errorf("%w; rollback failed: %w", err, rbErr)
		}
		return err
	}
	return nil
}

func pluginEntryNamed(entries []config.PluginEntry, name, scope string) (config.PluginEntry, bool) {
	for _, entry := range entries {
		if entry.Name != name {
			continue
		}
		if scope == "project" {
			entry.Source = config.MCPSourceProjectConfig
		} else {
			entry.Source = config.MCPSourceUserConfig
		}
		return entry, true
	}
	return config.PluginEntry{}, false
}

func (t *Tool) rollbackMCPReplace(act *action, previous config.PluginEntry, oldDisconnected, connected bool) error {
	if connected && act.disconnect != nil {
		act.disconnect()
		act.disconnect = nil
	}
	if oldDisconnected {
		return t.restoreMCP(previous)
	}
	return nil
}

func (t *Tool) restoreMCP(previous config.PluginEntry) error {
	if t.connectMCP == nil || previous.Name == "" {
		return nil
	}
	_, err := t.connectMCP(previous)
	return err
}

// applyRemoveSkill deletes a previously installed skill file or directory.
// We only touch the project/global skills dir directly; the .mcp.json /
// config file is not modified.
func (t *Tool) applyRemoveSkill(_ request, act *action) error {
	target := act.Target
	if target == "" {
		return newErr(ErrInvalidManifest, "remove_skill action is missing target")
	}
	if _, err := os.Lstat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			act.Target = ""
			return nil
		}
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	act.Target = ""
	return nil
}

func (t *Tool) applyRemoveSkillRoot(_ request, act *action) error {
	target := act.Target
	if target == "" {
		return newErr(ErrInvalidManifest, "remove_skill_root action is missing target")
	}
	unlock, err := config.LockConfigFileEdits(act.ConfigPath)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := config.LoadForEditReadOnlyStrict(act.ConfigPath)
	if err != nil {
		return err
	}
	removed, err := cfg.RemoveSkillPath(target)
	if err != nil {
		return err
	}
	if !removed {
		return nil
	}
	if err := cfg.SaveTo(act.ConfigPath); err != nil {
		return err
	}
	return nil
}

// applyRemoveMCP removes an MCP server entry from the active config and
// asks the host to disconnect it (if a connector is wired).
func (t *Tool) applyRemoveMCP(_ request, act *action) error {
	unlock, err := config.LockConfigFileEdits(act.ConfigPath)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := config.LoadForEditReadOnlyStrict(act.ConfigPath)
	if err != nil {
		return err
	}
	if !cfg.RemovePlugin(act.Name) {
		return nil
	}
	if err := cfg.SaveTo(act.ConfigPath); err != nil {
		return err
	}
	if t.onDisconnect != nil {
		t.onDisconnect(act.Name)
	}
	return nil
}
