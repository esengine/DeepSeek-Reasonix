package configbackup

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/state/instruction"
)

// maxFileBytes bounds one skill or memory file. A backup is configuration,
// and a checked-in model or dataset inside a skill folder is not.
const maxFileBytes = 512 << 10

// accountTokenKey never travels: it is this machine's session, not a secret the
// user configured, and restoring it elsewhere would clone a login.
const accountTokenKey = "REASONIX_ACCOUNT_TOKEN"

// CollectOptions says what to put in a snapshot.
type CollectOptions struct {
	Categories []Category
	AppVersion string
	Now        time.Time
}

// Collect reads this machine's user-level setup into a snapshot.
func Collect(opts CollectOptions) (*Snapshot, error) {
	if len(opts.Categories) == 0 {
		return nil, ErrNoCategories
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	s := &Snapshot{
		Format: FormatVersion, CreatedAt: now.UTC(), AppVersion: opts.AppVersion,
		Platform: runtime.GOOS + "/" + runtime.GOARCH, Categories: opts.Categories,
	}
	c := newCollector(slices.Contains(opts.Categories, CategorySecrets))
	if err := c.load(); err != nil {
		return nil, err
	}
	for _, cat := range opts.Categories {
		if err := c.collect(cat); err != nil {
			return nil, err
		}
	}
	s.Items, s.Omitted = c.items, c.omitted
	return s, nil
}

type collector struct {
	withSecrets bool
	cfg         *config.Config
	items       []Item
	omitted     []Omission
}

func newCollector(withSecrets bool) *collector { return &collector{withSecrets: withSecrets} }

func (c *collector) load() error {
	cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	if err != nil {
		return err
	}
	c.cfg = cfg
	return nil
}

func (c *collector) add(kind, name string, data any) error {
	raw, ok := data.(json.RawMessage)
	if !ok {
		var err error
		if raw, err = json.Marshal(data); err != nil {
			return err
		}
	}
	c.items = append(c.items, Item{ID: itemID(kind, name), Category: categoryOfKind(kind), Kind: kind, Name: name, Data: raw})
	return nil
}

func (c *collector) collect(cat Category) error {
	switch cat {
	case CategorySettings:
		return c.settings()
	case CategoryExtensions:
		return c.extensions()
	case CategoryMemory:
		return c.memory()
	case CategoryAutomation:
		return c.automation()
	case CategorySecrets:
		return c.secrets()
	}
	return ErrUnknownCategory
}

func (c *collector) settings() error {
	if err := c.add(KindGeneral, "general", generalData{DefaultModel: c.cfg.DefaultModel, Language: c.cfg.Language}); err != nil {
		return err
	}
	for _, p := range c.cfg.Providers {
		entry := config.ProviderEntryConfigSnapshot(p)
		if !c.withSecrets {
			_, entry.Headers = stripSecrets(entry.Name, nil, entry.Headers)
			for _, u := range []*string{&entry.BaseURL, &entry.ChatURL, &entry.RequestURL, &entry.ModelsURL, &entry.BalanceURL} {
				*u = stripURLSecrets(*u)
			}
		}
		raw, err := encodeTOML(entry)
		if err != nil {
			return err
		}
		if err := c.add(KindProvider, entry.Name, raw); err != nil {
			return err
		}
	}
	raw, err := encodeTOML(lookOf(c.cfg))
	if err != nil {
		return err
	}
	return c.add(KindInterface, "interface", raw)
}

func (c *collector) extensions() error {
	if err := c.skills(); err != nil {
		return err
	}
	st, err := pluginpkg.LoadState(config.ReasonixHomeDir())
	if err != nil {
		return err
	}
	for _, p := range st.Plugins {
		if err := c.add(KindPlugin, p.Name, pluginData{Source: stripURLSecrets(p.Source), Version: p.Version, Commit: p.Commit}); err != nil {
			return err
		}
	}
	for _, p := range c.cfg.Plugins {
		entry := p
		if !c.withSecrets {
			entry.Env, entry.Headers = stripSecrets(entry.Name, entry.Env, entry.Headers)
			entry.URL = stripURLSecrets(entry.URL)
			entry.Args = secrets.RedactArgs(entry.Args)
		}
		raw, err := encodeTOML(entry)
		if err != nil {
			return err
		}
		if err := c.add(KindMCP, entry.Name, raw); err != nil {
			return err
		}
	}
	return nil
}

func (c *collector) skills() error {
	root := skillsRoot()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !validSegment(name) {
			continue
		}
		files := c.readTree(KindSkill, root, name)
		if len(files) == 0 {
			continue
		}
		if err := c.add(KindSkill, name, skillData{Files: files}); err != nil {
			return err
		}
	}
	return nil
}

// readTree reads the regular files at or under root/rel. Symlinks are left out:
// on the other machine one would dangle or point somewhere it should not.
func (c *collector) readTree(kind, root, rel string) []fileData {
	var out []fileData
	_ = filepath.WalkDir(filepath.Join(root, rel), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		r, _ := filepath.Rel(root, p)
		r = filepath.ToSlash(r)
		if d.IsDir() {
			if skipDirs[d.Name()] || (r != "." && strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			c.omitted = append(c.omitted, Omission{Kind: kind, Name: r, Reason: OmitHidden})
			return nil
		}
		if !d.Type().IsRegular() {
			c.omitted = append(c.omitted, Omission{Kind: kind, Name: r, Reason: OmitNotFile})
			return nil
		}
		if f, ok := c.readFile(kind, p, r); ok {
			out = append(out, f)
		}
		return nil
	})
	return out
}

var skipDirs = map[string]bool{".git": true, ".hg": true, ".svn": true, "node_modules": true}

func (c *collector) readFile(kind, abs, rel string) (fileData, bool) {
	info, err := os.Stat(abs)
	if err != nil {
		c.omitted = append(c.omitted, Omission{Kind: kind, Name: rel, Reason: OmitUnreadable})
		return fileData{}, false
	}
	if info.Size() > maxFileBytes {
		c.omitted = append(c.omitted, Omission{Kind: kind, Name: rel, Reason: OmitTooLarge})
		return fileData{}, false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		c.omitted = append(c.omitted, Omission{Kind: kind, Name: rel, Reason: OmitUnreadable})
		return fileData{}, false
	}
	return fileData{Path: rel, Data: data}, true
}

func (c *collector) memory() error {
	docs := memoryDocsRoot()
	if docs == "" {
		return nil
	}
	for _, name := range memoryDocNames() {
		abs := filepath.Join(docs, name)
		if info, err := os.Lstat(abs); err != nil || !info.Mode().IsRegular() {
			continue
		}
		if f, ok := c.readFile(KindMemory, abs, name); ok {
			if err := c.add(KindMemory, memoryRootDocs+"/"+name, memoryData{Root: memoryRootDocs, fileData: f}); err != nil {
				return err
			}
		}
	}
	for _, f := range c.readTree(KindMemory, memoryFactsRoot(), ".") {
		f.Path = strings.TrimPrefix(f.Path, "./")
		if err := c.add(KindMemory, memoryRootFacts+"/"+f.Path, memoryData{Root: memoryRootFacts, fileData: f}); err != nil {
			return err
		}
	}
	return nil
}

func (c *collector) automation() error {
	settings, err := globalHooks()
	if err != nil {
		return err
	}
	for _, event := range sortedEvents(settings) {
		for _, h := range settings.Hooks[event] {
			if strings.TrimSpace(h.Command) == "" {
				continue
			}
			if !c.withSecrets {
				h.Env, _ = stripSecrets(hookName(string(event), h), h.Env, nil)
			}
			if err := c.add(KindHook, hookName(string(event), h), hookData{Event: string(event), Hook: h}); err != nil {
				return err
			}
		}
	}
	if cmd := strings.TrimSpace(c.cfg.Statusline.Command); cmd != "" {
		return c.add(KindStatusline, "statusline", statuslineData{Command: cmd})
	}
	return nil
}

// secrets carries the stored keys the saved providers point at. Only keys in
// Reasonix's own credential store travel; a key set in the shell environment
// is that environment's, not this setup's.
func (c *collector) secrets() error {
	seen := map[string]bool{}
	for _, p := range c.cfg.Providers {
		key := strings.TrimSpace(p.APIKeyEnv)
		if key == "" || key == accountTokenKey || seen[key] || !config.CredentialStored(key) {
			continue
		}
		seen[key] = true
		value := config.ResolveCredential(key).Value
		if value == "" {
			continue
		}
		if err := c.add(KindSecret, key, secretData{Key: key, Value: value}); err != nil {
			return err
		}
	}
	return nil
}

func globalHooks() (hook.Settings, error) {
	var s hook.Settings
	body, err := fileencoding.ReadFileUTF8(hook.GlobalSettingsPath(""))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return s, err
	}
	return s, nil
}

func sortedEvents(s hook.Settings) []hook.Event {
	events := make([]hook.Event, 0, len(s.Hooks))
	for e := range s.Hooks {
		events = append(events, e)
	}
	slices.Sort(events)
	return events
}

func memoryDocNames() []string {
	return append(slices.Clone(instruction.DocumentNames), instruction.LocalDocumentNames...)
}
