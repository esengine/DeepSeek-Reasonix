package configbackup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
)

var (
	ErrConsentRequired = errors.New("configbackup: some items need consent before they are restored")
	ErrUnknownItem     = errors.New("configbackup: the backup has no such item")
)

// ConsentError names the selected items that were not consented to.
type ConsentError struct{ IDs []string }

func (e *ConsentError) Error() string {
	return fmt.Sprintf("%v: %s", ErrConsentRequired, strings.Join(e.IDs, ", "))
}

func (e *ConsentError) Is(target error) bool { return target == ErrConsentRequired }

// ApplyRequest is the person's decision on one preview.
type ApplyRequest struct {
	PlanID    string   `json:"planId"`
	Items     []string `json:"items"`
	Consented []string `json:"consented"`
}

// PluginRef is a plugin to bring back through the plugin install flow.
type PluginRef struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

// ItemFailure is one selected item that could not be written.
type ItemFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// ApplyResult reports what landed.
type ApplyResult struct {
	Applied []string      `json:"applied"`
	Plugins []PluginRef   `json:"plugins,omitempty"`
	Failed  []ItemFailure `json:"failed,omitempty"`
}

// Apply writes the selected items of a held preview. It refuses the whole
// request, writing nothing and keeping the preview, when a selected item needs
// consent it was not given.
func (p *Planner) Apply(req ApplyRequest) (*ApplyResult, error) {
	s, err := p.peek(req.PlanID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Item, len(s.Items))
	for _, it := range s.Items {
		byID[it.ID] = it
	}
	var chosen []Item
	for _, id := range req.Items {
		it, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownItem, id)
		}
		chosen = append(chosen, it)
	}
	local, err := collectLocal(s)
	if err != nil {
		return nil, err
	}
	var unconsented []string
	for _, it := range chosen {
		if consentFor(it, s, local) != "" && !slices.Contains(req.Consented, it.ID) {
			unconsented = append(unconsented, it.ID)
		}
	}
	if len(unconsented) > 0 {
		return nil, &ConsentError{IDs: unconsented}
	}
	if _, err := p.take(req.PlanID); err != nil {
		return nil, err
	}
	return write(chosen), nil
}

func (p *Planner) peek(id string) (*Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h, ok := p.plans[id]
	if !ok || p.now().After(h.expires) {
		return nil, ErrPlanExpired
	}
	return h.snapshot, nil
}

func write(items []Item) *ApplyResult {
	res := &ApplyResult{Applied: []string{}}
	var cfgItems, hooks []Item
	for _, it := range items {
		switch it.Kind {
		case KindGeneral, KindProvider, KindInterface, KindMCP, KindStatusline:
			cfgItems = append(cfgItems, it)
		case KindHook:
			hooks = append(hooks, it)
		case KindPlugin:
			var d pluginData
			if err := json.Unmarshal(it.Data, &d); err != nil {
				res.fail(it.ID, err)
				continue
			}
			res.Plugins = append(res.Plugins, PluginRef{Name: it.Name, Source: d.Source, Version: d.Version, Commit: d.Commit})
		default:
			res.record(it.ID, writeOne(it))
		}
	}
	if len(cfgItems) > 0 {
		err := config.EditConfigFile(config.UserConfigPath(), func(cfg *config.Config) error {
			for _, it := range cfgItems {
				if err := applyConfigItem(cfg, it); err != nil {
					return fmt.Errorf("%s: %w", it.ID, err)
				}
			}
			return nil
		})
		for _, it := range cfgItems {
			res.record(it.ID, err)
		}
	}
	if len(hooks) > 0 {
		err := applyHooks(hooks)
		for _, it := range hooks {
			res.record(it.ID, err)
		}
	}
	return res
}

func (r *ApplyResult) record(id string, err error) {
	if err != nil {
		r.fail(id, err)
		return
	}
	r.Applied = append(r.Applied, id)
}

func (r *ApplyResult) fail(id string, err error) {
	r.Failed = append(r.Failed, ItemFailure{ID: id, Error: err.Error()})
}

func applyConfigItem(cfg *config.Config, it Item) error {
	switch it.Kind {
	case KindGeneral:
		var g generalData
		if err := json.Unmarshal(it.Data, &g); err != nil {
			return err
		}
		cfg.DefaultModel, cfg.Language = g.DefaultModel, g.Language
		return nil
	case KindInterface:
		var look interfaceSettings
		if err := decodeTOML(it.Data, &look); err != nil {
			return err
		}
		look.applyTo(cfg)
		return nil
	case KindProvider:
		var entry config.ProviderEntry
		if err := decodeTOML(it.Data, &entry); err != nil {
			return err
		}
		if entry.Name != it.Name {
			return fmt.Errorf("%w: provider %q", ErrMalformed, it.Name)
		}
		for _, have := range cfg.Providers {
			if have.Name == entry.Name && endpoints(secretsRedacted(have)) == endpoints(entry) {
				entry.BaseURL, entry.ChatURL, entry.RequestURL = have.BaseURL, have.ChatURL, have.RequestURL
				entry.ModelsURL, entry.BalanceURL = have.ModelsURL, have.BalanceURL
				entry.Headers = keepLocalSecrets(entry.Headers, have.Headers)
			}
		}
		return cfg.UpsertProvider(entry)
	case KindMCP:
		var entry config.PluginEntry
		if err := decodeTOML(it.Data, &entry); err != nil {
			return err
		}
		if entry.Name != it.Name {
			return fmt.Errorf("%w: MCP server %q", ErrMalformed, it.Name)
		}
		for _, have := range cfg.Plugins {
			if holdsLocal(entry, have) {
				entry.URL, entry.Args = have.URL, have.Args
				entry.Env = keepLocalSecrets(entry.Env, have.Env)
				entry.Headers = keepLocalSecrets(entry.Headers, have.Headers)
			}
		}
		return cfg.UpsertPlugin(entry)
	case KindStatusline:
		var s statuslineData
		if err := json.Unmarshal(it.Data, &s); err != nil {
			return err
		}
		cfg.Statusline.Command = s.Command
		return nil
	}
	return fmt.Errorf("%w: kind %q", ErrUnknownItem, it.Kind)
}

// applyHooks adds each restored hook that is not already configured. A hook
// is never replaced: the name is derived from what it runs, so "the same
// hook" with a different command is a different hook.
func applyHooks(items []Item) error {
	settings, err := globalHooks()
	if err != nil {
		return err
	}
	if settings.Hooks == nil {
		settings.Hooks = map[hook.Event][]hook.HookConfig{}
	}
	for _, it := range items {
		var d hookData
		if err := json.Unmarshal(it.Data, &d); err != nil {
			return err
		}
		event := hook.Event(d.Event)
		if !hook.IsKnownEvent(d.Event) {
			return fmt.Errorf("%s: unknown hook event %q", it.ID, d.Event)
		}
		if slices.ContainsFunc(settings.Hooks[event], func(h hook.HookConfig) bool {
			return hookName(d.Event, h) == hookName(d.Event, d.Hook)
		}) {
			continue
		}
		settings.Hooks[event] = append(settings.Hooks[event], d.Hook)
	}
	return hook.Save(hook.ScopeGlobal, "", settings)
}

func writeOne(it Item) error {
	switch it.Kind {
	case KindSkill:
		var d skillData
		if err := json.Unmarshal(it.Data, &d); err != nil {
			return err
		}
		if !validSegment(it.Name) {
			return fmt.Errorf("%w: %q", ErrUnsafePath, it.Name)
		}
		for _, f := range d.Files {
			if first, _, _ := strings.Cut(f.Path, "/"); first != it.Name {
				return fmt.Errorf("%w: %q is outside skill %q", ErrUnsafePath, f.Path, it.Name)
			}
		}
		for _, f := range d.Files {
			if err := writeFile(skillsRoot(), f); err != nil {
				return err
			}
		}
		return nil
	case KindMemory:
		var d memoryData
		if err := json.Unmarshal(it.Data, &d); err != nil {
			return err
		}
		if d.Root == memoryRootDocs && !slices.Contains(memoryDocNames(), d.Path) {
			return fmt.Errorf("%w: %q is not a standing-instruction file", ErrUnsafePath, d.Path)
		}
		return writeFile(memoryRoot(d.Root), d.fileData)
	case KindSecret:
		var d secretData
		if err := json.Unmarshal(it.Data, &d); err != nil {
			return err
		}
		if d.Key != it.Name || deniedKey(d.Key) || !config.IsValidCredentialKey(d.Key) {
			return fmt.Errorf("%w: credential %q", ErrUnknownItem, d.Key)
		}
		_, err := config.SetCredential(d.Key, d.Value)
		return err
	}
	return fmt.Errorf("%w: kind %q", ErrUnknownItem, it.Kind)
}

func writeFile(root string, f fileData) error {
	if root == "" {
		return fmt.Errorf("%w: no destination root", ErrUnsafePath)
	}
	dest, err := safeJoin(root, f.Path)
	if err != nil {
		return err
	}
	if len(f.Data) > maxFileBytes {
		return ErrTooLarge
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if _, err := safeJoin(root, f.Path); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(dest, f.Data, 0o644)
}
