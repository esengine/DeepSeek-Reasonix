package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		Trusted:     s == string(hook.ScopeGlobal) || hook.IsTrusted(root, ""),
		Hooks:       []HookConfigView{},
		Events:      hookEventNames(),
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
	return writeHooksSettingsFile(path, settings)
}

func (a *App) TrustProjectHooks() error {
	return a.TrustProjectHooksForRoot(a.activeHookProjectRoot())
}

func (a *App) TrustProjectHooksForRoot(root string) error {
	root = strings.TrimSpace(root)
	if strings.TrimSpace(root) == "" || root == "." {
		return fmt.Errorf("no active project workspace")
	}
	return hook.Trust(root, "")
}

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
	for _, e := range hook.Events {
		if event == e {
			return true
		}
	}
	return false
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
	body, err := os.ReadFile(path)
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
	if body, err := os.ReadFile(path); err == nil {
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
	return os.WriteFile(path, body, 0o644)
}
