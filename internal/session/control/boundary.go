// boundary.go — the editable tool boundary: which calls need approval before
// they run, and where an approved one is allowed to write.
package control

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/safety/sandbox"
)

// PermissionLists is one layer of the fine-grained gate: three ordered lists
// plus the fallback a call falls through to. Deny is checked first and is the
// only entry no approval prompt can talk its way past.
type PermissionLists struct {
	Mode  string   `json:"mode"`
	Allow []string `json:"allow"`
	Ask   []string `json:"ask"`
	Deny  []string `json:"deny"`
}

func (l PermissionLists) of(name string) []string {
	switch name {
	case "allow":
		return l.Allow
	case "ask":
		return l.Ask
	}
	return l.Deny
}

// PermissionRules is what an editor needs: the lists it may write, the file
// they land in, and — only when a project config declares its own — the merge
// that is actually in force, which an edit here cannot move.
type PermissionRules struct {
	PermissionLists
	Path       string           `json:"path"`
	ShadowedBy string           `json:"shadowedBy,omitempty"`
	Effective  *PermissionLists `json:"effective,omitempty"`
	// Granted is what was allowed for this session alone, on a prompt rather
	// than in the file. Nothing wrote it down, so a reader with only the file
	// in front of them is looking at less than the agent may currently do.
	Granted []string `json:"granted,omitempty"`
	// Dormant lists rules in the file that name no tool, so they match nothing.
	Dormant []event.DormantPermissionRule `json:"dormant,omitempty"`
	// Remembered rules are stored per workspace outside project configuration.
	Remembered          []string `json:"remembered,omitempty"`
	RememberedPath      string   `json:"rememberedPath,omitempty"`
	RememberedErrorCode string   `json:"rememberedErrorCode,omitempty"`
}

// SandboxSettings is where an approved write may land, and whether bash runs
// jailed. EffectiveWriteRoots is the expansion the confiner will use: an empty
// workspace root is not "unconfined", it is "the session directory", and an
// editor that showed the blank would be describing a different machine.
type SandboxSettings struct {
	Bash          string   `json:"bash"`
	Network       bool     `json:"network"`
	WorkspaceRoot string   `json:"workspaceRoot"`
	AllowWrite    []string `json:"allowWrite"`
	// ProtectChangedFiles refuses a whole-file write over a change made since
	// the agent last read or wrote that file.
	ProtectChangedFiles bool `json:"protectChangedFiles"`

	EffectiveWriteRoots []string `json:"effectiveWriteRoots"`
	// EffectiveBash is the mode the confiner will use. Windows forces off, an
	// unset value enforces elsewhere, and a project file outranks this one — so
	// Bash above is what was written and this is what will run.
	EffectiveBash string `json:"effectiveBash"`
	// Available reports whether this host has an OS sandbox at all. Where it is
	// false, enforce refuses every bash call rather than running it unconfined,
	// so the editor has to say so instead of offering a dead switch.
	Available bool   `json:"available"`
	Why       string `json:"why,omitempty"`
	WhyCode   string `json:"whyCode,omitempty"`
	Platform  string `json:"platform"`

	Path       string `json:"path"`
	ShadowedBy string `json:"shadowedBy,omitempty"`
}

// PermissionRules reads the user layer this editor writes, and reports the
// effective merge beside it when a project file outranks it.
func (c *Controller) PermissionRules() PermissionRules {
	path := config.UserConfigPath()
	out := PermissionRules{
		PermissionLists: listsFrom(config.LoadForEdit(path)),
		Path:            path,
		ShadowedBy:      shadowingConfig(path, c.WorkspaceRoot()),
		Granted:         c.approval.sessionGrants(),
	}
	out.Dormant = DormantRules(c.PermissionVocabulary(), out.PermissionLists)
	if root := c.WorkspaceRoot(); root != "" {
		store := config.NewProjectGrantStore(config.Roots{}.Home())
		out.RememberedPath = store.Path()
		if grant, err := store.Grant(root); err != nil {
			if errors.Is(err, config.ErrProjectGrantsUnavailable) {
				out.RememberedErrorCode = "project_grants.unavailable"
			}
		} else {
			out.Remembered = grant.Allow
		}
	}
	if out.ShadowedBy == "" {
		return out
	}
	if cfg, err := config.LoadForRootReadOnly(c.WorkspaceRoot()); err == nil {
		if merged := listsFrom(cfg); !sameLists(merged, out.PermissionLists) {
			out.Effective = &merged
		}
	}
	return out
}

// RevokeSessionGrant takes back what a prompt allowed for this session, one
// rule or all of them, and reports how many it took. What a person allowed once
// they can stop allowing without ending the session — otherwise the only way
// back is to close it, which costs them the work as well as the grant.
func (c *Controller) RevokeSessionGrant(rule string) int {
	return c.approval.revokeSessionGrant(rule)
}

// RevokeRememberedProjectRule removes one persistent allow rule for this
// workspace. The store resolves its fingerprint and preserves other grants.
// The host rebuilds the runtime after this write so the active gate follows it.
// A successful revoke also drops a matching same-session grant before the
// rebuild snapshots authorizations for the replacement controller.
func (c *Controller) RevokeRememberedProjectRule(rule string) error {
	root := c.WorkspaceRoot()
	if root == "" || strings.TrimSpace(rule) == "" {
		return fmt.Errorf("%w: workspace and rule are required", config.ErrProjectGrantsUnavailable)
	}
	store := config.NewProjectGrantStore(config.Roots{}.Home())
	if err := store.Update(root, func(grant config.ProjectGrant) (config.ProjectGrant, error) {
		grant.Allow = slices.DeleteFunc(grant.Allow, func(saved string) bool { return saved == rule })
		return grant, nil
	}); err != nil {
		return err
	}
	c.approval.revokeSessionGrant(rule)
	return nil
}

// SavePermissionRules replaces the three lists wholesale after validating every
// rule with the parser the gate itself uses, so a typo is refused on the screen
// that made it rather than silently never matching anything.
func (c *Controller) SavePermissionRules(in PermissionLists) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetPermissionMode(in.Mode); err != nil {
		return err
	}
	saved := listsFrom(cfg)
	cfg.Permissions.Allow = nil
	cfg.Permissions.Ask = nil
	cfg.Permissions.Deny = nil
	vocab := c.PermissionVocabulary()
	for _, list := range []struct {
		name  string
		rules []string
	}{{"allow", in.Allow}, {"ask", in.Ask}, {"deny", in.Deny}} {
		for _, rule := range list.rules {
			if strings.TrimSpace(rule) == "" {
				continue
			}
			if !slices.Contains(saved.of(list.name), rule) {
				if err := vocab.Check(list.name, rule); err != nil {
					return err
				}
			}
			if err := cfg.AddPermissionRule(list.name, rule); err != nil {
				return err
			}
		}
	}
	return cfg.SaveTo(path)
}

// ErrSandboxUnavailable is the one refusal this editor makes on its own. A
// caller has to tell "this host cannot enforce" apart from "the config file
// would not write", and only the producer of the refusal knows which it was.
var ErrSandboxUnavailable = errors.New("bash sandbox requested but unavailable on this host")

// SandboxSettings reads the configured jail beside what it expands to here.
func (c *Controller) SandboxSettings() SandboxSettings {
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	out := SandboxSettings{
		Bash:                strings.TrimSpace(cfg.Sandbox.Bash),
		Network:             cfg.Sandbox.Network,
		WorkspaceRoot:       strings.TrimSpace(cfg.Sandbox.WorkspaceRoot),
		AllowWrite:          append([]string{}, cfg.Sandbox.AllowWrite...),
		ProtectChangedFiles: cfg.Tools.ChangedFilesProtected(),
		Available:           sandbox.Available(),
		Platform:            runtime.GOOS,
		Path:                path,
		ShadowedBy:          shadowingConfig(path, c.WorkspaceRoot()),
	}
	if !out.Available {
		out.Why, out.WhyCode = sandbox.UnavailableMessage(), sandbox.UnavailableCode()
	}
	effective := cfg
	if merged, err := config.LoadForRootReadOnly(c.WorkspaceRoot()); err == nil {
		effective = merged
	}
	out.EffectiveWriteRoots = effective.WriteRootsForRoot(c.WorkspaceRoot())
	out.EffectiveBash = effective.BashMode()
	return out
}

// SaveSandboxSettings persists the jail. The caller rebuilds: boot binds the
// write roots into every file tool while assembling, so a live runtime keeps
// the boundary it was built with until it is replaced.
func (c *Controller) SaveSandboxSettings(in SandboxSettings) error {
	bash := strings.ToLower(strings.TrimSpace(in.Bash))
	switch bash {
	case "", "enforce", "off":
	default:
		return fmt.Errorf("sandbox bash %q: must be enforce|off", in.Bash)
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	// Only asking for the jail is refused where there is none. A file already
	// holding enforce, which the rendered template writes, keeps saving its
	// other fields; bash itself stays refused at run time either way.
	if bash == "enforce" && strings.TrimSpace(cfg.Sandbox.Bash) != "enforce" && !sandbox.Available() {
		return fmt.Errorf("%w: %s", ErrSandboxUnavailable, sandbox.UnavailableRemediation())
	}
	cfg.Sandbox.Bash = bash
	cfg.Sandbox.Network = in.Network
	cfg.Sandbox.WorkspaceRoot = strings.TrimSpace(in.WorkspaceRoot)
	cfg.Sandbox.AllowWrite = trimmedList(in.AllowWrite)
	protect := in.ProtectChangedFiles
	cfg.Tools.ProtectChangedFiles = &protect
	return cfg.SaveTo(path)
}

func listsFrom(cfg *config.Config) PermissionLists {
	mode := strings.TrimSpace(cfg.Permissions.Mode)
	if mode == "" {
		mode = "ask"
	}
	return PermissionLists{
		Mode:  mode,
		Allow: append([]string{}, cfg.Permissions.Allow...),
		Ask:   append([]string{}, cfg.Permissions.Ask...),
		Deny:  append([]string{}, cfg.Permissions.Deny...),
	}
}

func sameLists(a, b PermissionLists) bool {
	return a.Mode == b.Mode &&
		strings.Join(a.Allow, "\x00") == strings.Join(b.Allow, "\x00") &&
		strings.Join(a.Ask, "\x00") == strings.Join(b.Ask, "\x00") &&
		strings.Join(a.Deny, "\x00") == strings.Join(b.Deny, "\x00")
}

// shadowingConfig names the project file that outranks writePath, or "" when
// the file being edited is the one in force.
func shadowingConfig(writePath, root string) string {
	effective := config.SourcePathForRoot(root)
	if effective == "" || samePath(effective, writePath) {
		return ""
	}
	if abs, err := filepath.Abs(effective); err == nil {
		return abs
	}
	return effective
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}

func trimmedList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
