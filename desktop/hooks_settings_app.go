package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/hook"
)

type HookConfigView struct {
	Event       string `json:"event"`
	Match       string `json:"match,omitempty"`
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
}

type HooksSettingsView struct {
	Scope       string           `json:"scope"`
	Path        string           `json:"path"`
	ProjectRoot string           `json:"projectRoot"`
	Trusted     bool             `json:"trusted"`
	Hooks       []HookConfigView `json:"hooks"`
	Events      []string         `json:"events"`
}

func (a *App) HooksSettings(scope string) HooksSettingsView {
	s, path, root := normalizeHooksScope(scope, a.activeHookProjectRoot())
	view := HooksSettingsView{
		Scope:       s,
		Path:        path,
		ProjectRoot: root,
		Hooks:       []HookConfigView{},
		Events:      hookEventNames(),
	}
	// Project hooks run only once approved as they stand now; what this view
	// showed is what the Approve button may approve.
	program, pending := hook.PendingProjectHooks(hook.LoadOptions{ProjectRoot: root})
	view.Trusted = s != string(hook.ScopeProject) || !pending
	if !view.Trusted {
		shownProjectHooks.Store(root, program.Digest)
	}
	settings, err := readHooksSettingsFile(path)
	if err != nil || settings.Hooks == nil {
		return view
	}
	for _, event := range hook.Events {
		for _, cfg := range settings.Hooks[event] {
			if strings.TrimSpace(cfg.Command) == "" {
				continue
			}
			view.Hooks = append(view.Hooks, hookConfigView(event, cfg))
		}
	}
	return view
}

func (a *App) SaveHooksSettings(scope string, hooks []HookConfigView) error {
	return a.SaveHooksSettingsForRoot(scope, a.activeHookProjectRoot(), hooks)
}

func (a *App) SaveHooksSettingsForRoot(scope, projectRoot string, hooks []HookConfigView) error {
	s, path, _ := normalizeHooksScope(scope, projectRoot)
	settings := hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{}}
	for _, h := range hooks {
		event := hook.Event(strings.TrimSpace(h.Event))
		if !validHookEvent(event) {
			return fmt.Errorf("unknown hook event %q", h.Event)
		}
		cmd := strings.TrimSpace(h.Command)
		if cmd == "" {
			continue
		}
		cmd = hook.NormalizeCommand(cmd)
		settings.Hooks[event] = append(settings.Hooks[event], hook.HookConfig{
			Match:       strings.TrimSpace(h.Match),
			Command:     cmd,
			Description: strings.TrimSpace(h.Description),
			Timeout:     h.Timeout,
			Cwd:         strings.TrimSpace(h.Cwd),
		})
	}
	if s == string(hook.ScopeProject) && strings.TrimSpace(path) == "" {
		return fmt.Errorf("no active project workspace")
	}
	if err := writeHooksSettingsFile(path, settings); err != nil {
		return err
	}
	// Saving from the editor is the person choosing these hooks as written.
	if s == string(hook.ScopeProject) {
		return hook.ApproveProjectHooksAs(hook.LoadOptions{ProjectRoot: projectRoot}, settings)
	}
	return nil
}

func (a *App) TrustProjectHooks() error {
	return a.TrustProjectHooksForRoot(a.activeHookProjectRoot())
}

// errHooksNotShown refuses approving hooks that carry fields this editor
// cannot display: a person may only approve what they were shown.
var errHooksNotShown = errors.New("these project hooks set env or contextFile, which this editor does not show; review and approve them with `reasonix trust`")

// TrustProjectHooksForRoot approves root's project hooks as they stand now.
func (a *App) TrustProjectHooksForRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("no active project workspace")
	}
	settings, err := readHooksSettingsFile(hook.ProjectSettingsPath(root))
	if err != nil {
		return err
	}
	for _, list := range settings.Hooks {
		for _, cfg := range list {
			if len(cfg.Env) > 0 || strings.TrimSpace(cfg.ContextFile) != "" {
				return errHooksNotShown
			}
		}
	}
	program, ok := hook.ProjectHooksProgram(root)
	if !ok {
		return nil
	}
	if shown, _ := shownProjectHooks.Load(root); shown != program.Digest {
		return errHooksChangedSinceShown
	}
	return config.NewProjectProgramStore(config.ReasonixHomeDir()).Approve(root, program)
}

// shownProjectHooks is the digest of the project hooks each root's settings
// view last showed as waiting, so approving never covers a later rewrite.
var shownProjectHooks sync.Map

var errHooksChangedSinceShown = errors.New("these project hooks changed after they were shown; reload them and review again")

func (a *App) activeHookProjectRoot() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab := a.activeTabLocked(); tab != nil && tab.Scope == "project" {
		return strings.TrimSpace(tab.WorkspaceRoot)
	}
	return ""
}

func normalizeHooksScope(scope, projectRoot string) (string, string, string) {
	if strings.EqualFold(strings.TrimSpace(scope), string(hook.ScopeProject)) {
		root := strings.TrimSpace(projectRoot)
		if root == "" {
			return string(hook.ScopeProject), "", ""
		}
		return string(hook.ScopeProject), hook.ProjectSettingsPath(root), root
	}
	return string(hook.ScopeGlobal), hook.GlobalSettingsPath(""), ""
}

func hookEventNames() []string {
	out := make([]string, 0, len(hook.Events))
	for _, event := range hook.Events {
		out = append(out, string(event))
	}
	return out
}

func validHookEvent(event hook.Event) bool {
	return slices.Contains(hook.Events, event)
}

func hookConfigView(event hook.Event, cfg hook.HookConfig) HookConfigView {
	return HookConfigView{
		Event:       string(event),
		Match:       cfg.Match,
		Command:     cfg.Command,
		Description: cfg.Description,
		Timeout:     cfg.Timeout,
		Cwd:         cfg.Cwd,
	}
}

func readHooksSettingsFile(path string) (hook.Settings, error) {
	var settings hook.Settings
	body, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return settings, err
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		return settings, err
	}
	if settings.Hooks == nil {
		settings.Hooks = map[hook.Event][]hook.HookConfig{}
	}
	return settings, nil
}

func writeHooksSettingsFile(path string, settings hook.Settings) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("empty hooks settings path")
	}
	raw := map[string]json.RawMessage{}
	if body, err := fileencoding.ReadFileUTF8(path); err == nil {
		if err := json.Unmarshal(body, &raw); err != nil {
			return err
		}
	}
	hooksJSON, err := json.Marshal(settings.Hooks)
	if err != nil {
		return err
	}
	raw["hooks"] = hooksJSON
	body, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, body, 0o644)
}
