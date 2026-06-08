package installsource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/skill"
)

// apply dispatches to the per-action implementation. Each branch is
// responsible for setting act.Status / act.Error / act.Next and for
// cleaning up any partial side effects it left behind.
func (t *installSourceTool) apply(ctx context.Context, req request, act *action) error {
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
	default:
		return fmt.Errorf("unknown install action kind %q", act.Kind)
	}
}

// applySkillRoot appends the path to the active config's [skills].paths and
// re-builds the Store to confirm the listed skills are discoverable.
func (t *installSourceTool) applySkillRoot(req request, act *action) error {
	cfg := config.LoadForEdit(act.ConfigPath)
	if err := cfg.AddSkillPath(act.Source); err != nil {
		return err
	}
	if err := cfg.SaveTo(act.ConfigPath); err != nil {
		return err
	}
	store := skill.New(skill.Options{HomeDir: t.home, ProjectRoot: t.root, CustomPaths: append(cfg.SkillCustomPaths(), act.Source)})
	for _, name := range act.Skills {
		sk, ok := store.Read(name)
		if !ok {
			return newErr(ErrSourceUnreadable, "skill %q was registered but is not discoverable", name)
		}
		act.Discoverable = true
		if act.CanonicalPath == "" && sk.Path != "" {
			act.CanonicalPath = sk.Path
		}
		if strings.TrimSpace(sk.Description) == "" {
			act.Warnings = append(act.Warnings, fmt.Sprintf("skill %q has no description frontmatter; it is installed but the skills index will use a placeholder", name))
		}
	}
	for _, listed := range store.List() {
		for _, name := range act.Skills {
			if listed.Name == name {
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
func (t *installSourceTool) applyCopySkill(req request, act *action) error {
	canonical, err := t.skillCanonicalPath(act.skill.Name, req.Scope)
	if err != nil {
		return err
	}
	targetDir := filepath.Dir(canonical)
	conflicts, err := t.skillConflictTargets(act.skill.Name, req.Scope)
	if err != nil {
		return err
	}
	for _, conflict := range conflicts {
		if _, err := os.Lstat(conflict); err == nil {
			return newErr(ErrAlreadyExists, "skill %q already exists at %s", act.skill.Name, conflict)
		}
	}
	if act.skill.IsDir {
		if err := copyDir(act.skill.SourcePath, targetDir); err != nil {
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
	return t.verifySkill(req.Scope, act.skill.Name, act)
}

// applyLinkSkill creates a symlink in the skills dir pointing at the source.
// Absolute sources outside the project or home root are blocked even when the
// plan was approved: a link-mode skill should not become a backdoor to arbitrary
// host files.
func (t *installSourceTool) applyLinkSkill(req request, act *action) error {
	canonical, err := t.skillCanonicalPath(act.skill.Name, req.Scope)
	if err != nil {
		return err
	}
	target := canonical
	if act.skill.IsDir {
		target = filepath.Dir(canonical)
	}
	conflicts, err := t.skillConflictTargets(act.skill.Name, req.Scope)
	if err != nil {
		return err
	}
	for _, conflict := range conflicts {
		if _, err := os.Lstat(conflict); err == nil {
			return newErr(ErrAlreadyExists, "skill %q already exists at %s", act.skill.Name, conflict)
		}
	}
	if !isLinkTargetSafe(act.skill.SourcePath, t.home, t.root) {
		act.RiskLevel = RiskHigh
		act.RiskReasons = append(act.RiskReasons, "link target is an absolute path outside the project or home root")
		return newErr(ErrUnsafeLinkTarget, "skill %q source %s is outside %s and %s", act.skill.Name, act.skill.SourcePath, t.root, t.home)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(act.skill.SourcePath, target); err != nil {
		return err
	}
	act.Target = target
	act.CanonicalPath = canonical
	return t.verifySkill(req.Scope, act.skill.Name, act)
}

// isLinkTargetSafe reports whether a symlink source is allowed. The link
// target is safe when:
//   - it is a relative path (we never follow the parent of a relative link),
//   - or its absolute form is contained within the user's home or the
//     project root.
//
// Absolute paths outside both scopes are rejected with ErrUnsafeLinkTarget
// so a SKILL.md that points at /etc/passwd does not silently succeed.
func isLinkTargetSafe(source, home, projectRoot string) bool {
	if source == "" {
		return false
	}
	if !filepath.IsAbs(source) {
		return true
	}
	clean := filepath.Clean(source)
	for _, root := range []string{home, projectRoot} {
		if root == "" {
			continue
		}
		base := filepath.Clean(root)
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
func (t *installSourceTool) applyInstallMCP(ctx context.Context, req request, act *action) error {
	if act.entry.Name == "" {
		return newErr(ErrInvalidManifest, "MCP action has no server entry")
	}
	cfg := config.LoadForEdit(act.ConfigPath)
	var previous config.PluginEntry
	hadPrevious := false
	for _, existing := range cfg.Plugins {
		if existing.Name == act.entry.Name {
			previous = existing
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
					return fmt.Errorf("%w; reconnect previous server failed: %v", err, rbErr)
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
	if err := cfg.UpsertPlugin(act.entry); err != nil {
		if rbErr := t.rollbackMCPReplace(act, previous, oldDisconnected, connected); rbErr != nil {
			return fmt.Errorf("%w; rollback failed: %v", err, rbErr)
		}
		return err
	}
	if err := cfg.SaveTo(act.ConfigPath); err != nil {
		if rbErr := t.rollbackMCPReplace(act, previous, oldDisconnected, connected); rbErr != nil {
			return fmt.Errorf("%w; rollback failed: %v", err, rbErr)
		}
		return err
	}
	return nil
}

func (t *installSourceTool) rollbackMCPReplace(act *action, previous config.PluginEntry, oldDisconnected, connected bool) error {
	if connected && act.disconnect != nil {
		act.disconnect()
		act.disconnect = nil
	}
	if oldDisconnected {
		return t.restoreMCP(previous)
	}
	return nil
}

func (t *installSourceTool) restoreMCP(previous config.PluginEntry) error {
	if t.connectMCP == nil || previous.Name == "" {
		return nil
	}
	_, err := t.connectMCP(previous)
	return err
}

// applyRemoveSkill deletes a previously installed skill file or directory.
// We only touch the project/global skills dir directly; the .mcp.json /
// config file is not modified.
func (t *installSourceTool) applyRemoveSkill(_ request, act *action) error {
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

func (t *installSourceTool) applyRemoveSkillRoot(_ request, act *action) error {
	target := act.Target
	if target == "" {
		return newErr(ErrInvalidManifest, "remove_skill_root action is missing target")
	}
	cfg := config.LoadForEdit(act.ConfigPath)
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
func (t *installSourceTool) applyRemoveMCP(_ request, act *action) error {
	cfg := config.LoadForEdit(act.ConfigPath)
	if !cfg.RemovePlugin(act.Name) {
		// Nothing to remove is not a failure: idempotent.
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
