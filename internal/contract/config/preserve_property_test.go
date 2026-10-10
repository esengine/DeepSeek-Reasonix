package config

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/base/testenv"
)

// userConfigGen writes a random user config in the shapes people and the 1.x
// line leave behind: unknown keys (named zz*) of every value form, comments,
// odd spacing, CRLF, a BOM, shuffled tables and arrays of tables.
type userConfigGen struct {
	r     *rand.Rand
	nl    string
	mixed bool
	used  map[string]bool
	owner string
	lines []ownedLine // single-line unknown assignments that must survive
}

type ownedLine struct{ owner, text string }

func (g *userConfigGen) pick(xs ...string) string { return xs[g.r.Intn(len(xs))] }
func (g *userConfigGen) chance(p float64) bool    { return g.r.Float64() < p }
func (g *userConfigGen) indent() string           { return g.pick("", "", "", "  ", "\t") }
func (g *userConfigGen) eq() string               { return g.pick(" = ", "=", "  =  ", " =\t") }

func (g *userConfigGen) eol() string {
	if g.mixed && g.chance(0.5) {
		return map[string]string{"\n": "\r\n", "\r\n": "\n"}[g.nl]
	}
	return g.nl
}

func (g *userConfigGen) tail() string {
	if g.chance(0.3) {
		return g.pick("   # note", " #x = 1", " # [desktop]", "#")
	}
	return ""
}

func (g *userConfigGen) comment(b *strings.Builder) {
	if g.chance(0.3) {
		b.WriteString(g.indent() + g.pick("# comment", "# [agent]", "# theme = \"x\"", "#") + g.eol())
	}
	if g.chance(0.3) {
		b.WriteString(g.eol())
	}
}

func (g *userConfigGen) unknownValue() (string, bool) {
	switch g.r.Intn(8) {
	case 0:
		return strconv.Itoa(g.r.Intn(1000)), true
	case 1:
		return g.pick(`"a#b"`, `"[desktop]"`, `"x = 1"`, `'lit # ]'`, `"q\"uote"`, `""`), true
	case 2:
		return "{ a = 1, b = [1, 2], c = { d = \"]\" } }", true
	case 3:
		return "[ { x = 1 }, { y = \"[z]\" } ]", true
	case 4:
		return "[" + g.eol() + "  \"a\", # ] c" + g.eol() + "  \"b\"," + g.eol() + "]", false
	case 5:
		return "\"\"\"" + g.eol() + "[desktop]" + g.eol() + "theme = \"light\"" + g.eol() + "[[providers]]" + g.eol() + "\"\"\"", false
	case 6:
		return "'''" + g.eol() + "[agent]" + g.eol() + "compact_ratio = 0.1 # x" + g.eol() + "'''", false
	default:
		return "true", true
	}
}

func (g *userConfigGen) unknownKeys(b *strings.Builder, n int) {
	for range n {
		k := g.pick("zz_a", "zz_b", "\"zz.q\"", "'zz lit'", "\"zz_键\"", "zz-dash")
		if g.used[k] {
			continue
		}
		g.used[k] = true
		v, single := g.unknownValue()
		line := g.indent() + k + g.eq() + v + g.tail()
		if single {
			g.lines = append(g.lines, ownedLine{g.owner, strings.TrimRight(line, " ")})
		}
		b.WriteString(line + g.eol())
		g.comment(b)
	}
}

func (g *userConfigGen) table(b *strings.Builder, name string, known ...string) {
	g.used = map[string]bool{}
	if g.chance(0.15) {
		name = " " + name + " "
	}
	b.WriteString(g.indent() + "[" + name + "]" + g.tail() + g.eol())
	g.comment(b)
	for _, i := range g.r.Perm(len(known)/2 + 1) {
		switch {
		case i == len(known)/2:
			g.unknownKeys(b, g.r.Intn(3))
		case g.chance(0.75):
			b.WriteString(g.indent() + known[2*i] + g.eq() + known[2*i+1] + g.tail() + g.eol())
			g.comment(b)
		}
	}
}

func (g *userConfigGen) provider(b *strings.Builder, name string) {
	g.used = map[string]bool{}
	g.owner = "p:" + name
	b.WriteString("[[providers]]" + g.eol())
	quote := g.pick(`"`, `"`, `'`)
	b.WriteString("name" + g.eq() + quote + name + quote + g.eol())
	b.WriteString("kind = \"openai\"" + g.eol() + "base_url = \"http://" + name + ".example/v1\"" + g.eol())
	b.WriteString("model = \"m-" + name + "\"" + g.tail() + g.eol())
	g.unknownKeys(b, 1+g.r.Intn(2))
	if g.chance(0.3) {
		b.WriteString(g.eol() + "[providers.headers]" + g.tail() + g.eol() + "X-A" + g.eq() + "\"1\"" + g.eol())
	}
	if g.chance(0.3) {
		b.WriteString("[providers.zz_sub]" + g.eol())
		g.used = map[string]bool{}
		g.unknownKeys(b, 1)
	}
	g.owner = ""
}

func (g *userConfigGen) host(b *strings.Builder, i int) {
	g.used = map[string]bool{}
	name := fmt.Sprintf("h%d", i)
	b.WriteString("[[remote.hosts]]" + g.eol() + "name = \"" + name + "\"" + g.eol())
	b.WriteString(fmt.Sprintf("host = \"10.0.0.%d\"", i+1) + g.eol())
	g.owner = "h:" + name
	g.unknownKeys(b, 1)
	g.owner = ""
}

func (g *userConfigGen) doc() string {
	var b strings.Builder
	if g.chance(0.1) {
		b.WriteString("\ufeff")
	}
	g.comment(&b)
	b.WriteString("config_version" + g.eq() + g.pick("12", "6", "12") + g.eol())
	if g.chance(0.5) {
		b.WriteString("language" + g.eq() + g.pick(`"zh"`, `"en"`) + g.eol())
	}
	g.used = map[string]bool{}
	g.unknownKeys(&b, g.r.Intn(3))
	items := `["model", "cost"]`
	if g.chance(0.4) {
		items = "[" + g.eol() + "  \"model\"," + g.eol() + "  \"cost\", # c" + g.eol() + "]"
	}
	prompt := "\"\"\"" + g.eol() + "You are X." + g.eol() + "[desktop]" + g.eol() + "theme = 'y'" + g.eol() + "\"\"\""
	tables := []func(){
		func() {
			g.table(&b, "ui", "theme", g.pick(`"auto"`, `"dark"`), "show_turn_usage", g.pick("true", "false"))
		},
		func() {
			g.table(&b, "desktop", "theme", g.pick(`"auto"`, `"dark"`, `"light"`), "language", g.pick(`"zh"`, `"en"`),
				"status_bar_items", items, "check_updates", g.pick("true", "false"),
				"default_tool_approval_mode", g.pick(`"ask"`, `"auto"`, `"yolo"`, `"danger-full-access"`, `"workspace-write"`))
		},
		func() { g.table(&b, "agent", "compact_ratio", g.pick("0.5", "0.7", "0.85"), "system_prompt", prompt) },
		func() { g.table(&b, "notifications", "enabled", g.pick("true", "false")) },
		func() { g.table(&b, "network", "proxy_mode", g.pick(`"auto"`, `"env"`, `"off"`)) },
		func() { g.table(&b, "tools", "bash_timeout_seconds", g.pick("60", "120", "0")) },
		func() {
			g.table(&b, "zz_tab")
			if g.chance(0.5) {
				g.table(&b, "zz_tab.sub")
			}
		},
		func() { g.table(&b, "checkpoints", "zz_retain", "7") },
	}
	// Tables shuffle; the elements of each array keep their order among themselves.
	type slot struct{ kind, n int }
	var slots []slot
	for i := range tables {
		slots = append(slots, slot{0, i})
	}
	for range g.r.Intn(4) {
		slots = append(slots, slot{1, 0})
	}
	for range g.r.Intn(3) {
		slots = append(slots, slot{2, 0})
	}
	g.r.Shuffle(len(slots), func(i, j int) { slots[i], slots[j] = slots[j], slots[i] })
	np, nh := 0, 0
	for _, s := range slots {
		switch s.kind {
		case 0:
			tables[s.n]()
		case 1:
			g.provider(&b, fmt.Sprintf("p%d", np))
			np++
		default:
			g.host(&b, nh)
			nh++
		}
	}
	if out := b.String(); !g.chance(0.15) {
		return out
	}
	return strings.TrimRight(b.String(), "\r\n")
}

type configEdit struct {
	desc string
	fn   func(*Config) error
}

func (g *userConfigGen) edits(c *Config) []configEdit {
	th, cr, pm := g.pick("auto", "dark", "light"), []float64{0.6, 0.75, 0.85}[g.r.Intn(3)], g.pick("auto", "env", "off")
	bt, dl, am := 30+g.r.Intn(3)*30, g.pick("", "zh", "en"), g.pick("ask", "auto", "yolo")
	sbi, sp, rl := g.pick("model", "cost", "balance"), g.pick("", "Be terse.", "Line1\n[desktop]\ntheme = 'z'", `C:\dir "quoted"""`), g.pick("", "zh", "en")
	np, nh := fmt.Sprintf("new%d", g.r.Intn(3)), fmt.Sprintf("nh%d", g.r.Intn(1000))
	all := []configEdit{
		{"desktop.theme", func(c *Config) error { return c.SetDesktopAppearance(th, "") }},
		{"compact_ratio", func(c *Config) error { return c.SetCompactRatio(cr) }},
		{"notifications", func(c *Config) error { c.Notifications.Enabled = !c.Notifications.Enabled; return nil }},
		{"proxy_mode", func(c *Config) error { c.Network.ProxyMode = pm; return nil }},
		{"bash_timeout", func(c *Config) error { c.Tools.BashTimeoutSeconds = &bt; return nil }},
		{"desktop.language", func(c *Config) error { c.Desktop.Language = dl; return nil }},
		{"approval", func(c *Config) error { return c.SetDesktopDefaultToolApprovalMode(am) }},
		{"status_bar_items", func(c *Config) error { c.Desktop.StatusBarItems = []string{sbi}; return nil }},
		{"system_prompt", func(c *Config) error { c.Agent.SystemPrompt = sp; return nil }},
		{"root.language", func(c *Config) error { c.Language = rl; return nil }},
		{"add provider", func(c *Config) error {
			return c.UpsertProvider(ProviderEntry{Name: np, Kind: "openai", BaseURL: "http://n.example/v1", Model: "nm"})
		}},
		{"add host", func(c *Config) error {
			c.Remote.Hosts = append(c.Remote.Hosts, RemoteHostEntry{Name: nh, Host: "10.9.9.9"})
			return nil
		}},
	}
	var named []string
	for _, p := range c.Providers {
		if strings.HasPrefix(p.Name, "p") {
			named = append(named, p.Name)
		}
	}
	if len(named) > 0 {
		victim, hv := named[g.r.Intn(len(named))], g.pick("1", "2")
		all = append(all,
			configEdit{"remove provider " + victim, func(c *Config) error { _, err := c.RemoveProvider(victim); return err }},
			configEdit{"rename provider " + victim, renameProvider(victim, victim+"x")},
			configEdit{"reverse providers", func(c *Config) error {
				slices.Reverse(c.Providers)
				return nil
			}},
			configEdit{"edit provider " + victim, func(c *Config) error {
				for i := range c.Providers {
					if c.Providers[i].Name == victim {
						c.Providers[i].Model = "changed"
						c.Providers[i].Headers = map[string]string{"X-B": hv}
					}
				}
				return nil
			}})
	}
	if n := len(c.Remote.Hosts); n > 0 {
		i := g.r.Intn(n)
		all = append(all, configEdit{"remove host", func(c *Config) error {
			c.Remote.Hosts = slices.Delete(c.Remote.Hosts, i, i+1)
			return nil
		}})
	}
	var out []configEdit
	for _, i := range g.r.Perm(len(all))[:1+g.r.Intn(3)] {
		out = append(out, all[i])
	}
	return out
}

// unknownEntries keeps only the zz* keys and [checkpoints], with array
// elements keyed by name.
func unknownEntries(v any, keep bool) any {
	out := map[string]any{}
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if keep || strings.HasPrefix(k, "zz") || k == "checkpoints" {
				out[k] = x
			} else if sub := unknownEntries(x, false); sub != nil {
				out[k] = sub
			}
		}
	case []map[string]any:
		for _, el := range t {
			if sub := unknownEntries(el, false); sub != nil {
				out[fmt.Sprint(el["name"])] = sub
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// dropGone removes from the unknown entries the elements the edit removed.
func dropGone(want any, got *Config) any {
	m, _ := want.(map[string]any)
	if ps, ok := m["providers"].(map[string]any); ok {
		for name := range ps {
			if _, still := got.Provider(name); !still {
				delete(ps, name)
			}
		}
		if len(ps) == 0 {
			delete(m, "providers")
		}
	}
	remote, _ := m["remote"].(map[string]any)
	if hs, ok := remote["hosts"].(map[string]any); ok {
		for name := range hs {
			if !slices.ContainsFunc(got.Remote.Hosts, func(h RemoteHostEntry) bool { return h.Name == name }) {
				delete(hs, name)
			}
		}
		if len(hs) == 0 {
			delete(m, "remote")
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func ownerKept(owner string, got *Config) bool {
	kind, name, _ := strings.Cut(owner, ":")
	switch kind {
	case "p":
		_, ok := got.Provider(name)
		return ok
	case "h":
		return slices.ContainsFunc(got.Remote.Hosts, func(h RemoteHostEntry) bool { return h.Name == name })
	}
	return true
}

// checkRandomSave saves one random edit of one random file and fails unless
// the save is an in-place edit that loads as the edit, keeps every unknown
// entry of what survived byte for byte, and is a fixed point of a no-op save.
func checkRandomSave(t *testing.T, path string, seed int64) {
	g := &userConfigGen{r: rand.New(rand.NewSource(seed))}
	g.nl = g.pick("\n", "\n", "\r\n")
	g.mixed = g.chance(0.1)
	text := g.doc()
	var original map[string]any
	if _, err := toml.Decode(strings.TrimPrefix(text, "\ufeff"), &original); err != nil {
		t.Fatalf("seed %d: generator wrote invalid TOML: %v\n%s", seed, err, text)
	}
	backups, _ := filepath.Glob(path + ".rewrite-*")
	for _, f := range append(backups, path) {
		_ = os.Remove(f)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	edits := g.edits(LoadForEdit(path))
	apply := func(c *Config) error {
		for _, e := range edits {
			if err := e.fn(c); err != nil {
				return err
			}
		}
		return nil
	}
	want := LoadForEdit(path)
	if apply(want) != nil || EditConfigFile(path, apply) != nil {
		return
	}
	raw, _ := os.ReadFile(path)
	saved := string(raw)
	fail := func(format string, a ...any) {
		t.Helper()
		t.Fatalf("seed %d edits %v: %s\n--- original ---\n%s\n--- saved ---\n%s", seed, edits, fmt.Sprintf(format, a...), text, saved)
	}
	var tree map[string]any
	if _, err := toml.Decode(strings.TrimPrefix(saved, "\ufeff"), &tree); err != nil {
		fail("saved file does not parse: %v", err)
	}
	if backups, _ := filepath.Glob(path + ".rewrite-*"); len(backups) > 0 {
		fail("left a backup: the save could not carry every entry")
	}
	got, gerr := loadUserConfigBytes(path, saved)
	wantLoaded, werr := loadUserConfigBytes(path, RenderTOMLForScope(want, RenderScopeUser))
	if gerr != nil || werr != nil || RenderTOMLForScope(got, RenderScopeUser) != RenderTOMLForScope(wantLoaded, RenderScopeUser) {
		fail("saved file does not load as the edit (%v, %v)", gerr, werr)
	}
	if w, s := dropGone(unknownEntries(original, false), got), unknownEntries(tree, false); !reflect.DeepEqual(w, s) {
		fail("unknown entries changed:\nwant %#v\ngot  %#v", w, s)
	}
	norm := strings.ReplaceAll(saved, "\r\n", "\n")
	for _, l := range g.lines {
		if ownerKept(l.owner, got) && !strings.Contains(norm, strings.ReplaceAll(l.text, "\r\n", "\n")) {
			fail("unknown line not kept byte for byte: %q", l.text)
		}
	}
	if err := EditConfigFile(path, func(*Config) error { return nil }); err != nil {
		fail("no-op save: %v", err)
	}
	if again, _ := os.ReadFile(path); string(again) != saved {
		fail("no-op save changed the file:\n%s", again)
	}
}

func isolateUserConfig(tb testing.TB) string {
	home := testenv.TempDir(tb)
	tb.Setenv("REASONIX_HOME", home)
	tb.Setenv("HOME", home)
	tb.Setenv("USERPROFILE", home)
	return UserConfigPath()
}

// REASONIX_PRESERVE_SEEDS widens the run, e.g. to 20000 before touching the
// patcher.
func TestRandomUserConfigSavesEditInPlace(t *testing.T) {
	path := isolateUserConfig(t)
	n := int64(150)
	if s := os.Getenv("REASONIX_PRESERVE_SEEDS"); s != "" {
		n, _ = strconv.ParseInt(s, 10, 64)
	}
	for seed := range n {
		checkRandomSave(t, path, seed)
	}
}

func FuzzUserConfigSave(f *testing.F) {
	path := isolateUserConfig(f)
	for _, seed := range []int64{0, 7, 42, 355, 1 << 40} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed int64) { checkRandomSave(t, path, seed) })
}
